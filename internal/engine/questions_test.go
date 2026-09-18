package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sigame/internal/packs"
)

func TestStakeQuestion(t *testing.T) {
	stake := mkQ(200, packs.QStake, "s")
	pack := mkPack(mkRound("R", packs.RoundStandard,
		mkTheme("T1", mkQ(100, packs.QSimple, "a"), stake, mkQ(300, packs.QSimple, "c")),
		mkTheme("T2", mkQ(100, packs.QSimple, "d"))))
	h := newH(t, pack, DefaultRules(), "p1", "p2", "p3", "p4")
	h.start("p1")
	h.setScore("p1", 500)
	h.setScore("p2", 300)
	h.setScore("p3", 1000)
	h.setScore("p4", 150) // ≤ nominal: not a participant
	h.choose(0, 1)
	require.Equal(t, SubStakes, h.q().Sub)
	ps := payload[PersonStakePayload](t, h.one(EvPersonStake))
	require.Equal(t, "p4", ps.PersonID)
	require.Equal(t, StakePass, ps.Mode)
	ask := payload[AskStakePayload](t, h.one(EvAskStake))
	require.Equal(t, "p1", ask.PersonID) // chooser opens
	require.Equal(t, []StakeMode{StakeNominal, StakeStake, StakeAllIn}, ask.Modes)
	require.Equal(t, 200, ask.Min)
	require.Equal(t, 500, ask.Max)
	require.Equal(t, 100, ask.Step)
	h.timerStart(TimerStakeMaking)

	// Opener cannot pass; others cannot bid out of turn.
	h.fail(Command{Type: CmdSetStake, Actor: player("p1"), StakeMode: StakePass}, ErrBadArgument)
	h.fail(Command{Type: CmdPass, Actor: player("p1")}, ErrNotAllowed)
	h.fail(Command{Type: CmdSetStake, Actor: player("p2"), StakeMode: StakeNominal}, ErrNotAllowed)
	h.stake("p1", StakeNominal, 0)
	require.Equal(t, 200, payload[PersonStakePayload](t, h.one(EvPersonStake)).Amount)

	// Next: lowest score among the rest (p2=300).
	ask = payload[AskStakePayload](t, h.one(EvAskStake))
	require.Equal(t, "p2", ask.PersonID)
	require.Equal(t, []StakeMode{StakePass, StakeStake, StakeAllIn}, ask.Modes)
	require.Equal(t, 300, ask.Min)
	require.Equal(t, 300, ask.Max)
	h.stake("p2", StakeStake, 300)

	// p3 must raise to ≥400 in steps of 100.
	ask = payload[AskStakePayload](t, h.one(EvAskStake))
	require.Equal(t, "p3", ask.PersonID)
	require.Equal(t, 400, ask.Min)
	h.fail(Command{Type: CmdSetStake, Actor: player("p3"), StakeMode: StakeStake, Amount: 450}, ErrBadArgument)
	h.fail(Command{Type: CmdSetStake, Actor: player("p3"), StakeMode: StakeStake, Amount: 300}, ErrBadArgument)
	h.stake("p3", StakeStake, 400)
	// p2 (300 ≤ 400) is auto-passed; p1 is asked again (cyclic).
	var passed []string
	for _, e := range h.ev(EvPersonStake) {
		if p := payload[PersonStakePayload](t, e); p.Mode == StakePass {
			passed = append(passed, p.PersonID)
		}
	}
	require.Equal(t, []string{"p2"}, passed)
	ask = payload[AskStakePayload](t, h.one(EvAskStake))
	require.Equal(t, "p1", ask.PersonID)
	require.Equal(t, 500, ask.Min)
	h.stake("p1", StakeAllIn, 0)
	require.Equal(t, 500, h.q().Stakes.Stake)

	// All-in can only be beaten by another all-in.
	ask = payload[AskStakePayload](t, h.one(EvAskStake))
	require.Equal(t, "p3", ask.PersonID)
	require.Equal(t, []StakeMode{StakePass, StakeAllIn}, ask.Modes)
	h.fail(Command{Type: CmdSetStake, Actor: player("p3"), StakeMode: StakeStake, Amount: 600}, ErrBadArgument)
	h.stake("p3", StakeAllIn, 0)
	sc := payload[SetChooserPayload](t, h.one(EvSetChooser))
	require.Equal(t, "p3", sc.PersonID)
	require.Equal(t, "stakeWinner", sc.Reason)
	require.Equal(t, 1000, payload[QuestionCaptionPayload](t, h.one(EvQuestionCaption)).Price)
	require.Equal(t, SubContent, h.q().Sub)
	h.runContent()
	require.Equal(t, "p3", payload[AskAnswerPayload](t, h.one(EvAskAnswer)).PersonID)
	h.timerStart(TimerSoloAnswering)
	h.answer("p3", "bad")
	h.validate(false)
	require.Equal(t, 0, h.score("p3")) // wrong all-in → 0
	h.one(EvRightAnswer)
}

func TestStakeTimeoutsAndEarlyPass(t *testing.T) {
	pack := mkPack(mkRound("R", packs.RoundStandard,
		mkTheme("T1", mkQ(100, packs.QStake, "s"), mkQ(200, packs.QStake, "s2"))))
	h := newH(t, pack, DefaultRules(), "p1", "p2", "p3")
	h.start("p1")
	h.setScore("p2", 500)
	h.setScore("p3", 500)
	h.choose(0, 0)
	// p1 (0 < nominal) is forced to nominal, no prompt.
	require.Equal(t, 100, h.q().Stakes.Stake)
	// p2 and p3 tie at 500 → showman picks the next bidder.
	sel := payload[AskSelectPlayerPayload](t, h.one(EvAskSelectPlayer))
	require.Equal(t, "staker", sel.Reason)
	require.ElementsMatch(t, []string{"p2", "p3"}, sel.Candidates)
	h.timeout(TimerShowmanDecision) // random pick
	ask := payload[AskStakePayload](t, h.one(EvAskStake))
	first := ask.PersonID
	other := "p2"
	if first == "p2" {
		other = "p3"
	}
	// Early voluntary pass by the other bidder.
	h.do(Command{Type: CmdPass, Actor: player(other)})
	require.False(t, h.q().Stakes.Active[other])
	// Bid timeout → pass → only the leader (p1) remains → p1 wins with 100.
	h.timeout(TimerStakeMaking)
	require.Equal(t, "p1", h.q().AnswererID)
	require.Equal(t, 100, h.q().CurPriceRight)
}

func TestSecretQuestion(t *testing.T) {
	secret := mkQ(200, packs.QSecret, "cat")
	secret.Params.Theme = "Secret theme"
	secret.Params.SelectionMode = packs.SelectExceptCurrent
	secret.Params.Price = &packs.NumberSet{Min: 0, Max: 0} // round min/max
	pack := mkPack(mkRound("R", packs.RoundStandard,
		mkTheme("T1", mkQ(100, packs.QSimple, "a"), secret, mkQ(300, packs.QSimple, "c"))))
	h := newH(t, pack, DefaultRules(), "p1", "p2", "p3")
	h.start("p1")
	h.choose(0, 1)
	require.Equal(t, SubSecretTransfer, h.q().Sub)
	sel := payload[AskSelectPlayerPayload](t, h.one(EvAskSelectPlayer))
	require.Equal(t, "secretTransfer", sel.Reason)
	require.Equal(t, "p1", sel.DeciderID)
	require.ElementsMatch(t, []string{"p2", "p3"}, sel.Candidates)
	h.timerStart(TimerPlayerSelection)
	h.none(EvQuestionCaption) // theme hidden until the transfer
	h.fail(Command{Type: CmdSelectPlayer, Actor: player("p1"), PersonID: "p1"}, ErrBadArgument)
	h.fail(Command{Type: CmdSelectPlayer, Actor: player("p2"), PersonID: "p3"}, ErrNotAllowed)
	h.do(Command{Type: CmdSelectPlayer, Actor: player("p1"), PersonID: "p3"})
	sc := payload[SetChooserPayload](t, h.one(EvSetChooser))
	require.Equal(t, "p3", sc.PersonID)
	require.Equal(t, "secretRecipient", sc.Reason)
	require.Equal(t, "Secret theme", payload[QuestionCaptionPayload](t, h.one(EvQuestionCaption)).Theme)
	ask := payload[AskStakePayload](t, h.one(EvAskStake))
	require.Equal(t, "p3", ask.PersonID)
	require.Equal(t, "secretPrice", ask.Reason)
	require.Equal(t, 100, ask.Min)
	require.Equal(t, 300, ask.Max)
	require.Equal(t, SubPriceSelect, h.q().Sub)
	h.fail(Command{Type: CmdSetStake, Actor: player("p3"), StakeMode: StakeStake, Amount: 200}, ErrBadArgument)
	h.stake("p3", StakeStake, 300)
	require.Equal(t, 300, payload[QuestionCaptionPayload](t, h.one(EvQuestionCaption)).Price)
	h.runContent()
	require.Equal(t, "p3", payload[AskAnswerPayload](t, h.one(EvAskAnswer)).PersonID)
	h.answer("p3", "cat")
	h.validate(true)
	require.Equal(t, 300, h.score("p3"))
	h.reveal()
	require.Equal(t, "p3", h.st().ChooserID)
}

func TestSecretVariants(t *testing.T) {
	pub := mkQ(200, packs.QSecretPublicPrice, "x")
	pub.Params.Price = &packs.NumberSet{Min: 500, Max: 500}
	noq := mkQ(200, packs.QSecretNoQuestion, "y")
	noq.Params.Price = &packs.NumberSet{Min: 100, Max: 300, Step: 100}
	noq.Params.SelectionMode = packs.SelectExceptCurrent
	pack := mkPack(mkRound("R", packs.RoundStandard, mkTheme("T1", pub, noq, mkQ(300, packs.QSimple, "c"))))
	h := newH(t, pack, DefaultRules(), "p1", "p2")
	h.start("p1")

	// Public price: caption before the transfer; selectionMode any → both candidates.
	h.choose(0, 0)
	require.Equal(t, 500, payload[QuestionCaptionPayload](t, h.one(EvQuestionCaption)).Price)
	sel := payload[AskSelectPlayerPayload](t, h.one(EvAskSelectPlayer))
	require.ElementsMatch(t, []string{"p1", "p2"}, sel.Candidates)
	h.timeout(TimerPlayerSelection) // random other than chooser → p2
	require.Equal(t, "p2", h.q().AnswererID)
	h.runContent()
	h.answer("p2", "no")
	h.validate(false)
	require.Equal(t, -500, h.score("p2"))
	h.reveal()

	// No question: single candidate (exceptCurrent, 2 players) → automatic; price choice, then award.
	require.Equal(t, "p2", h.st().ChooserID)
	h.choose(0, 1)
	require.Equal(t, "p1", h.q().AnswererID)
	ask := payload[AskStakePayload](t, h.one(EvAskStake))
	require.Equal(t, []int{100, 200, 300}, h.q().PriceChoices)
	require.Equal(t, 100, ask.Step)
	h.timeout(TimerStakeMaking) // minimum
	require.Equal(t, 100, h.score("p1"))
	require.Equal(t, "secret", payload[PersonScorePayload](t, h.one(EvPersonScore)).Reason)
	h.one(EvQuestionEnd)
	require.Equal(t, "p1", h.st().ChooserID)
	h.none(EvRightAnswer)
}

func TestNoRisk(t *testing.T) {
	pack := mkPack(mkRound("R", packs.RoundStandard, mkTheme("T1", mkQ(200, packs.QNoRisk, "nr"), mkQ(200, packs.QNoRisk, "nr2"))))
	h := newH(t, pack, DefaultRules(), "p1", "p2")
	h.start("p1")
	h.choose(0, 0)
	require.Equal(t, 400, payload[QuestionCaptionPayload](t, h.one(EvQuestionCaption)).Price)
	require.Equal(t, 400, h.q().CurPriceRight)
	require.Equal(t, 0, h.q().CurPriceWrong)
	h.runContent()
	require.Equal(t, "p1", payload[AskAnswerPayload](t, h.one(EvAskAnswer)).PersonID)
	h.answer("p1", "wrong")
	h.validate(false)
	require.Equal(t, 0, h.score("p1"))
	h.none(EvPersonScore)
	require.Equal(t, PStateWrong, h.p("p1").State)
	h.reveal()
	h.choose(0, 1)
	h.runContent()
	h.answer("p1", "nr2")
	h.do(Command{Type: CmdValidate, Actor: sm, Right: true, Factor: 0.5})
	require.Equal(t, 200, h.score("p1"))
}

func TestForAll(t *testing.T) {
	pack := mkPack(mkRound("R", packs.RoundStandard, mkTheme("T1", mkQ(100, packs.QForAll, "fa"), mkQ(200, packs.QForAll, "fb"))))
	h := newH(t, pack, DefaultRules(), "p1", "p2", "p3")
	h.start("p1")
	h.choose(0, 0)
	h.runContent()
	require.Equal(t, SubHiddenAnswering, h.q().Sub)
	require.Equal(t, int64(45000), payload[FinalThinkPayload](t, h.one(EvFinalThink)).DurationMs)
	require.Len(t, h.ev(EvAskAnswer), 3)
	require.True(t, payload[AskAnswerPayload](t, h.one(EvAskAnswer)).Hidden)
	h.timerStart(TimerHiddenAnswering)

	h.answer("p1", "fa")
	require.Equal(t, ToShowman(), h.one(EvPlayerAnswer).Audience) // hidden from other players
	require.True(t, payload[AskValidatePayload](t, h.one(EvAskValidate)).Preview)
	require.Equal(t, PStateHasAnswered, payload[PlayerStatePayload](t, h.one(EvPlayerState)).State)
	h.fail(Command{Type: CmdAnswer, Actor: player("p1"), Text: "again"}, ErrBadState)
	// Showman pre-validates p1 before the reveal.
	h.do(Command{Type: CmdValidate, Actor: sm, PersonID: "p1", Right: true})
	h.answer("p2", "zzz")
	h.timeout(TimerHiddenAnswering) // p3 did not answer

	// Reveal: p1 auto (pre-verdict), p2 needs the showman.
	var answers []string
	for _, e := range h.ev(EvPlayerAnswer) {
		require.Equal(t, ToAll(), e.Audience)
		answers = append(answers, payload[PlayerAnswerPayload](t, e).PersonID)
	}
	require.Equal(t, []string{"p1", "p2"}, answers)
	require.Equal(t, 100, h.score("p1"))
	require.Equal(t, "p2", payload[AskValidatePayload](t, h.one(EvAskValidate)).PersonID)
	require.Equal(t, SubValidating, h.q().Sub)
	h.validate(false)
	require.Equal(t, -100, h.score("p2"))
	// p3 revealed as "-" and penalised automatically.
	require.Equal(t, "-", payload[PlayerAnswerPayload](t, h.one(EvPlayerAnswer)).Answer.Text)
	require.Equal(t, -100, h.score("p3"))
	h.one(EvRightAnswer)
	h.reveal()
	require.Equal(t, "p1", h.st().ChooserID) // chooser unchanged

	// Penalty none for forAll.
	h.g.st.Rules.ForAllPenalty = PenaltyNone
	h.choose(0, 1)
	h.runContent()
	h.answer("p1", "x")
	h.answer("p2", "x")
	h.answer("p3", "x")
	require.Equal(t, SubValidating, h.q().Sub)
	h.validate(false) // p1; p2/p3 answered identically → cached verdict
	require.Equal(t, 100, h.score("p1"))
	require.Equal(t, -100, h.score("p2"))
	h.one(EvRightAnswer)
}

func TestSelectAnswerType(t *testing.T) {
	q := mkQ(100, packs.QSimple, "B")
	q.Params.AnswerType = packs.AnswerSelect
	q.Params.AnswerOptions = []packs.AnswerOption{
		{Label: "A", Content: []packs.ContentItem{{Type: packs.ContentText, Text: "one"}}},
		{Label: "B", Content: []packs.ContentItem{{Type: packs.ContentText, Text: "two"}}},
		{Label: "C", Content: []packs.ContentItem{{Type: packs.ContentText, Text: "three"}}},
	}
	pack := mkPack(mkRound("R", packs.RoundStandard, mkTheme("T1", q)))
	h := newH(t, pack, DefaultRules(), "p1", "p2", "p3")
	h.start("p1")
	h.choose(0, 0)
	h.runContent()
	opts := payload[AnswerOptionsPayload](t, h.one(EvAnswerOptions))
	require.Len(t, opts.Options, 3)
	h.win("p1")
	h.do(Command{Type: CmdAnswer, Actor: player("p1"), OptionLabel: "A"})
	v := payload[ValidationPayload](t, h.one(EvValidation))
	require.False(t, v.Right)
	require.Equal(t, "A", v.ExcludedOption)
	require.Equal(t, "auto", v.Source)
	h.one(EvButtonArmRequest)
	h.win("p2")
	h.do(Command{Type: CmdAnswer, Actor: player("p2"), OptionLabel: "C"})
	require.Equal(t, -100, h.score("p2"))
	h.one(EvRightAnswer) // one option left → question ends
	h.none(EvButtonArmRequest)
}

func TestNumberPointClientAnswers(t *testing.T) {
	num := mkQ(100, packs.QNoRisk, "42")
	num.Params.AnswerType = packs.AnswerNumber
	num.Params.AnswerDeviation = 2
	pt := mkQ(100, packs.QNoRisk, "0.5,0.5")
	pt.Params.AnswerType = packs.AnswerPoint
	cl := mkQ(100, packs.QNoRisk, "x")
	cl.Params.AnswerType = packs.AnswerClient
	pack := mkPack(mkRound("R", packs.RoundStandard, mkTheme("T1", num, pt, cl)))
	h := newH(t, pack, DefaultRules(), "p1")
	h.start("p1")

	h.choose(0, 0)
	h.runContent()
	n := 43.5
	h.do(Command{Type: CmdAnswer, Actor: player("p1"), Number: &n})
	require.True(t, payload[ValidationPayload](t, h.one(EvValidation)).Right)
	require.Equal(t, 200, h.score("p1"))
	h.reveal()

	h.choose(0, 1)
	h.runContent()
	h.do(Command{Type: CmdAnswer, Actor: player("p1"), Point: &Point{X: 0.9, Y: 0.9}})
	require.False(t, payload[ValidationPayload](t, h.one(EvValidation)).Right)
	h.reveal()

	h.choose(0, 2)
	h.runContent()
	h.do(Command{Type: CmdAnswer, Actor: player("p1"), Right: true, RightSet: true})
	require.True(t, payload[ValidationPayload](t, h.one(EvValidation)).Right)
	require.Equal(t, 400, h.score("p1"))
}

func TestMediaWaitAndFalseStartOff(t *testing.T) {
	q := mkQ(100, packs.QSimple, "m")
	q.Params.Question = []packs.ContentItem{
		{Type: packs.ContentText, Text: "intro", NoWait: true},
		{Type: packs.ContentAudio, MediaID: "abc"},
		{Type: packs.ContentVideo, MediaID: "def", DurationMs: 4000},
		{Type: packs.ContentImage, MediaID: "img"},
	}
	pack := mkPack(mkRound("R", packs.RoundStandard, mkTheme("T1", q)))
	r := DefaultRules()
	r.FalseStart = false
	h := newH(t, pack, r, "p1", "p2")
	h.start("p1")
	h.choose(0, 0)
	// Buttons armed before content.
	arm := payload[ButtonArmRequestPayload](t, h.one(EvButtonArmRequest))
	require.Equal(t, int64(-1), arm.ThinkingRemainingMs)
	c := payload[ContentPayload](t, h.one(EvContent))
	require.Len(t, c.Items, 2) // text + audio grouped
	require.Equal(t, "abc", payload[MediaWaitPayload](t, h.one(EvMediaWait)).MediaID)
	require.Equal(t, int64(2250), h.timerStart(TimerMediaFallback).DurationMs) // min = text reading time
	require.Equal(t, packs.PlaceBackground, c.Items[1].Placement)

	// p1 presses during content: content paused; wrong → content resumes, re-arm.
	h.win("p1")
	require.Equal(t, "paused", payload[ContentStatePayload](t, h.one(EvContentState)).State)
	require.Equal(t, TimerMediaFallback, payload[TimerPausePayload](t, h.one(EvTimerPause)).Kind)
	h.answer("p1", "no")
	h.validate(false)
	require.Equal(t, "resumed", payload[ContentStatePayload](t, h.one(EvContentState)).State)
	h.one(EvTimerResume)
	require.Equal(t, SubContent, h.q().Sub)
	require.True(t, h.q().Armed)

	// Media completion per player; then explicit duration; then image.
	h.do(Command{Type: CmdMediaCompleted, Actor: player("p1")})
	h.none(EvContent)
	h.do(Command{Type: CmdMediaCompleted, Actor: player("p2")})
	require.Equal(t, int64(4000), h.timerStart(TimerContent).DurationMs)
	h.timeout(TimerContent)
	require.Equal(t, int64(5000), h.timerStart(TimerContent).DurationMs)
	h.timeout(TimerContent)
	// Content over with buttons already armed: thinking timer starts.
	require.Equal(t, SubButtonWait, h.q().Sub)
	h.timerStart(TimerButtonPressing)
	h.win("p2")
	h.answer("p2", "m")
	h.validate(true)
	require.Equal(t, 100, h.score("p2"))
}

func TestOralModeAndCustomType(t *testing.T) {
	custom := mkQ(100, "weird", "w")
	pack := mkPack(mkRound("R", packs.RoundStandard, mkTheme("T1", mkQ(100, packs.QSimple, "a"), custom)))
	r := DefaultRules()
	r.Oral = true
	h := newH(t, pack, r, "p1", "p2")
	h.start("p1")
	require.Len(t, h.ev(EvAskChoose), 2)                              // chooser + showman
	h.do(Command{Type: CmdChooseQuestion, Actor: sm, Theme: 0, Q: 0}) // showman chooses for the player
	h.runContent()
	h.win("p1")
	av := payload[AskValidatePayload](t, h.one(EvAskValidate))
	require.True(t, av.Oral)
	require.True(t, payload[AskAnswerPayload](t, h.one(EvAskAnswer)).Oral)
	h.timeout(TimerAnswering) // ignored in oral mode
	h.none(EvValidation)
	h.validate(true)
	require.Equal(t, 100, h.score("p1"))
	h.reveal()

	// Custom type: manual scoring by the showman, Next ends it.
	h.do(Command{Type: CmdChooseQuestion, Actor: sm, Theme: 0, Q: 1})
	require.False(t, h.q().Known)
	h.runContent()
	require.Equal(t, SubAnswering, h.q().Sub)
	h.fail(Command{Type: CmdAnswer, Actor: player("p2"), Text: "x"}, ErrBadState)
	h.do(Command{Type: CmdValidate, Actor: sm, PersonID: "p2", Right: true})
	require.Equal(t, 100, h.score("p2"))
	h.do(Command{Type: CmdNext, Actor: sm})
	h.one(EvRightAnswer)
}
