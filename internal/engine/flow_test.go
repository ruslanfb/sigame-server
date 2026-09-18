package engine

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"sigame/internal/packs"
)

func TestNewValidation(t *testing.T) {
	_, err := New(nil, DefaultRules(), DefaultTimeSettings(), []PlayerInit{{ID: "p1"}}, "sm", 1)
	require.ErrorIs(t, err, ErrBadArgument)
	_, err = New(mkPack(), DefaultRules(), DefaultTimeSettings(), []PlayerInit{{ID: "p1"}}, "sm", 1)
	require.ErrorIs(t, err, ErrBadArgument)
	_, err = New(stdPack(), DefaultRules(), DefaultTimeSettings(), nil, "sm", 1)
	require.ErrorIs(t, err, ErrBadArgument)
	_, err = New(stdPack(), DefaultRules(), DefaultTimeSettings(), []PlayerInit{{ID: "p1"}, {ID: "p1"}}, "sm", 1)
	require.ErrorIs(t, err, ErrBadArgument)
	_, err = New(stdPack(), Rules{Mode: "weird"}, DefaultTimeSettings(), []PlayerInit{{ID: "p1"}}, "sm", 1)
	require.ErrorIs(t, err, ErrBadArgument)
	g, err := New(stdPack(), Rules{}, DefaultTimeSettings(), []PlayerInit{{ID: "p1"}}, "sm", 1)
	require.NoError(t, err)
	require.Equal(t, ModeClassic, g.State().Rules.Mode)
	require.Equal(t, 2, g.State().Rules.ForYourselfFactor)
	require.Equal(t, StageLobby, g.State().Stage)
}

func TestClassicSimpleFlow(t *testing.T) {
	h := newH(t, stdPack(), DefaultRules(), "p1", "p2", "p3")

	// Lobby: players cannot start; host can.
	h.fail(Command{Type: CmdStart, Actor: player("p1")}, ErrNotAllowed)
	h.do(Command{Type: CmdPlayerReady, Actor: player("p1")})
	require.True(t, h.p("p1").Ready)

	h.do(Command{Type: CmdStart, Actor: host})
	h.one(EvRoundStart)
	h.one(EvTable)
	h.one(EvRoundContent)
	h.timerStart(TimerRound)
	sel := payload[AskSelectPlayerPayload](t, h.one(EvAskSelectPlayer))
	require.Equal(t, "chooser", sel.Reason)
	require.ElementsMatch(t, []string{"p1", "p2", "p3"}, sel.Candidates)
	require.Equal(t, ToPerson("sm"), h.one(EvAskSelectPlayer).Audience)
	h.timerStart(TimerShowmanDecision)
	require.Equal(t, StageSelecting, h.st().Stage)

	// Only the showman resolves the tie, and only among candidates.
	h.fail(Command{Type: CmdSelectPlayer, Actor: player("p1"), PersonID: "p1"}, ErrNotAllowed)
	h.fail(Command{Type: CmdSelectPlayer, Actor: sm, PersonID: "nobody"}, ErrBadArgument)
	h.do(Command{Type: CmdSelectPlayer, Actor: sm, PersonID: "p2"})
	require.Equal(t, "p2", h.st().ChooserID)
	require.Equal(t, "roundStart", payload[SetChooserPayload](t, h.one(EvSetChooser)).Reason)
	require.Equal(t, ToPerson("p2"), h.one(EvAskChoose).Audience)
	h.timerStart(TimerQuestionSelection)

	// Only the chooser selects; cell must be valid.
	h.fail(Command{Type: CmdChooseQuestion, Actor: player("p1"), Theme: 0, Q: 0}, ErrNotAllowed)
	h.fail(Command{Type: CmdChooseQuestion, Actor: player("p2"), Theme: 5, Q: 0}, ErrBadArgument)
	h.choose(0, 0)
	qs := payload[QuestionStartPayload](t, h.one(EvQuestionStart))
	require.Equal(t, 100, qs.Price)
	require.Equal(t, packs.QSimple, qs.Type)
	require.False(t, qs.IsDefault)
	require.True(t, h.st().Table[0].Questions[0].Played)
	require.Equal(t, SubContent, h.q().Sub)
	c := payload[ContentPayload](t, h.one(EvContent))
	require.Equal(t, "Question text?", c.Items[0].Text)
	require.Equal(t, int64(700+2000), h.timerStart(TimerContent).DurationMs) // 14 chars / 20 cps + reflection
	h.none(EvButtonArmRequest)                                               // false starts: armed after content

	// Content over → button armed for everyone.
	h.timeout(TimerContent)
	arm := payload[ButtonArmRequestPayload](t, h.one(EvButtonArmRequest))
	require.True(t, h.one(EvButtonArmRequest).IsInternal())
	require.ElementsMatch(t, []string{"p1", "p2", "p3"}, arm.EligiblePlayerIDs)
	require.Equal(t, int64(5000), arm.ThinkingRemainingMs)
	require.Equal(t, int64(5000), h.timerStart(TimerButtonPressing).DurationMs)
	require.Equal(t, SubButtonWait, h.q().Sub)

	// Nobody pressed.
	h.do(Command{Type: CmdButtonResult, Actor: system, Nobody: true})
	h.one(EvButtonDisarm)
	require.Equal(t, "a1", payload[RightAnswerPayload](t, h.one(EvRightAnswer)).Text)
	require.Equal(t, SubReveal, h.q().Sub)
	h.reveal()
	h.one(EvQuestionEnd)
	h.one(EvSums)
	require.Equal(t, StageSelecting, h.st().Stage)
	require.Equal(t, "p2", h.st().ChooserID) // unchanged
	require.Equal(t, 0, h.score("p1")+h.score("p2")+h.score("p3"))

	// Second question: wrong → re-arm → right → chooser changes.
	h.choose(0, 1)
	h.runContent()
	h.now += 2000 // 2 s of thinking elapsed
	h.win("p1")
	tp := payload[TimerPausePayload](t, h.one(EvTimerPause))
	require.Equal(t, TimerButtonPressing, tp.Kind)
	require.Equal(t, int64(3000), tp.RemainingMs)
	require.Equal(t, "p1", payload[AskAnswerPayload](t, h.one(EvAskAnswer)).PersonID)
	h.timerStart(TimerAnswering)
	require.Equal(t, SubAnswering, h.q().Sub)
	require.False(t, h.p("p1").CanPress)

	h.fail(Command{Type: CmdAnswer, Actor: player("p2"), Text: "x"}, ErrNotAllowed)
	h.do(Command{Type: CmdAnswerDraft, Actor: player("p1"), Text: "dra"})
	require.Equal(t, ToShowman(), h.one(EvAnswerDraft).Audience)
	h.answer("p1", "wrong")
	require.Equal(t, ToAll(), h.one(EvPlayerAnswer).Audience)
	av := payload[AskValidatePayload](t, h.one(EvAskValidate))
	require.Equal(t, ToShowman(), h.one(EvAskValidate).Audience)
	require.Equal(t, []string{"a2"}, av.Rights)
	require.Equal(t, int64(30000), av.AutoAfterMs)
	require.Equal(t, SubValidating, h.q().Sub)

	h.fail(Command{Type: CmdValidate, Actor: player("p2"), Right: true}, ErrNotAllowed)
	h.validate(false)
	require.Equal(t, -200, h.score("p1"))
	require.Equal(t, "penalty", payload[PersonScorePayload](t, h.one(EvPersonScore)).Reason)
	arm = payload[ButtonArmRequestPayload](t, h.one(EvButtonArmRequest))
	require.True(t, arm.Rearm)
	require.ElementsMatch(t, []string{"p2", "p3"}, arm.EligiblePlayerIDs)
	require.Equal(t, int64(3000), arm.ThinkingRemainingMs)
	require.Equal(t, int64(3000), payload[TimerPausePayload](t, h.one(EvTimerResume)).RemainingMs)

	// p1 cannot press again; stale buzzer decisions are rejected.
	h.fail(Command{Type: CmdButtonResult, Actor: system, WinnerID: "p1"}, ErrBadArgument)
	h.win("p3")
	h.answer("p3", "a2")
	h.validate(true)
	require.Equal(t, 200, h.score("p3"))
	require.Equal(t, "p3", h.st().ChooserID)
	require.Equal(t, "rightAnswer", payload[SetChooserPayload](t, h.one(EvSetChooser)).Reason)
	h.one(EvRightAnswer)
	h.reveal()
	require.Equal(t, "p3", payload[AskChoosePayload](t, h.one(EvAskChoose)).PersonID)
	require.Equal(t, 2, h.st().QuestionsPlayed)
}

func TestNegativeScoresAndPassAndIgnoreWrong(t *testing.T) {
	h := newH(t, stdPack(), DefaultRules(), "p1", "p2")
	h.start("p1")
	h.choose(0, 2) // 300
	h.runContent()
	h.win("p1")
	h.answer("p1", "nope")
	h.validate(false)
	require.Equal(t, -300, h.score("p1"))
	// p2 passes → nobody can press → answer shown.
	h.do(Command{Type: CmdPass, Actor: player("p2")})
	h.one(EvPass)
	h.one(EvRightAnswer)
	h.reveal()

	// IgnoreWrong: no penalty at all.
	on := true
	h.fail(Command{Type: CmdSetOptions, Actor: player("p1"), Options: &RulesPatch{Oral: &on}}, ErrNotAllowed)
	h.g.st.Rules.IgnoreWrong = true
	h.choose(0, 1)
	h.runContent()
	h.win("p2")
	h.answer("p2", "nope")
	h.validate(false)
	require.Equal(t, 0, h.score("p2"))
	h.none(EvPersonScore)
}

func TestTimeouts(t *testing.T) {
	h := newH(t, stdPack(), DefaultRules(), "p1", "p2", "p3")
	h.start("p1")

	// Choice timeout → random cell.
	h.timeout(TimerQuestionSelection)
	h.one(EvQuestionStart)
	h.runContent()

	// Thinking timeout → nobody answered.
	h.timeout(TimerButtonPressing)
	h.one(EvButtonDisarm)
	h.one(EvRightAnswer)
	h.reveal()

	// Answer timeout → "-" wrong.
	h.choose(1, 0)
	h.runContent()
	h.win("p2")
	h.timeout(TimerAnswering)
	require.Equal(t, "-", payload[PlayerAnswerPayload](t, h.one(EvPlayerAnswer)).Answer.Text)
	v := payload[ValidationPayload](t, h.one(EvValidation))
	require.False(t, v.Right)
	require.Equal(t, "auto", v.Source)
	require.Equal(t, -100, h.score("p2"))
	h.one(EvButtonArmRequest)

	// Showman decision timeout → VALIDATION_TIMEOUT to the room, then auto verdict.
	h.win("p3")
	h.answer("p3", "maybe")
	h.timeout(TimerShowmanDecision)
	vt := h.one(EvValidationTimeout)
	require.True(t, vt.IsInternal())
	require.Equal(t, "maybe", payload[ValidationTimeoutPayload](t, vt).Answer)
	require.Equal(t, SubValidating, h.q().Sub)
	h.do(Command{Type: CmdValidate, Actor: system, PersonID: "p3", Right: true, Source: "auto"})
	require.Equal(t, 100, h.score("p3"))
	require.Equal(t, "auto", payload[ValidationPayload](t, h.one(EvValidation)).Source)

	// Stale timeout is ignored.
	evs, err := h.g.Apply(Command{Type: CmdTimeout, Actor: system, TimerID: "content-1"})
	require.NoError(t, err)
	require.Empty(t, evs)
	h.fail(Command{Type: CmdTimeout, Actor: player("p1"), TimerID: "x"}, ErrNotAllowed)
}

func TestPauseResume(t *testing.T) {
	h := newH(t, stdPack(), DefaultRules(), "p1", "p2")
	h.start("p1")
	h.fail(Command{Type: CmdShowmanPause, Actor: player("p1"), On: true}, ErrNotAllowed)
	h.choose(0, 0)
	h.runContent()
	h.now += 1000
	h.do(Command{Type: CmdShowmanPause, Actor: sm, On: true})
	require.True(t, h.st().Paused)
	require.True(t, payload[PausePayload](t, h.one(EvPause)).On)
	paused := map[string]int64{}
	for _, e := range h.ev(EvTimerPause) {
		p := payload[TimerPausePayload](t, e)
		paused[p.Kind] = p.RemainingMs
	}
	require.Equal(t, int64(4000), paused[TimerButtonPressing])
	require.Equal(t, int64(3_600_000-2700-1000), paused[TimerRound])

	// Player actions and buzzer results are frozen while paused.
	h.fail(Command{Type: CmdPass, Actor: player("p1")}, ErrBadState)
	evs, err := h.g.Apply(Command{Type: CmdButtonResult, Actor: system, WinnerID: "p1", AtMs: h.now})
	require.NoError(t, err)
	require.Empty(t, evs)
	evs, err = h.g.Apply(Command{Type: CmdTimeout, Actor: system, TimerID: h.timer(TimerButtonPressing).ID, AtMs: h.now})
	require.NoError(t, err)
	require.Empty(t, evs)
	// Showman may still edit scores while paused.
	h.setScore("p2", 50)
	require.Equal(t, 50, h.score("p2"))

	h.now += 60_000
	h.do(Command{Type: CmdShowmanPause, Actor: sm, On: false})
	require.False(t, h.st().Paused)
	var res []TimerPausePayload
	for _, e := range h.ev(EvTimerResume) {
		res = append(res, payload[TimerPausePayload](t, e))
	}
	require.Len(t, res, 2)
	tm := h.timer(TimerButtonPressing)
	require.Equal(t, int64(4000), tm.DurationMs)
	require.Equal(t, h.now, tm.StartedAtMs)
	require.False(t, tm.Paused)

	// Player actions are rejected while paused; a timer started while paused
	// (showman MOVE → reveal) is created paused and resumed later.
	h.win("p1")
	h.do(Command{Type: CmdShowmanPause, Actor: sm, On: true})
	h.do(Command{Type: CmdShowmanPause, Actor: sm, On: true}) // idempotent
	require.Empty(t, h.last)
	h.fail(Command{Type: CmdAnswer, Actor: player("p1"), Text: "a1"}, ErrBadState)
	h.do(Command{Type: CmdMove, Actor: sm, Dir: 1})
	h.timerStart(TimerReflection)
	require.True(t, h.timer(TimerReflection).Paused)
	require.Equal(t, TimerReflection, payload[TimerPausePayload](t, h.lastOf(EvTimerPause)).Kind)
	h.do(Command{Type: CmdShowmanPause, Actor: sm, On: false})
	require.False(t, h.timer(TimerReflection).Paused)
	h.reveal()
	h.one(EvQuestionEnd)
}

func TestPauseKeepsFrozenThinkingTimer(t *testing.T) {
	h := newH(t, stdPack(), DefaultRules(), "p1", "p2")
	h.start("p1")
	h.choose(0, 0)
	h.runContent()
	h.now += 1500
	h.win("p1") // thinking timer frozen with 3500 ms left
	h.do(Command{Type: CmdShowmanPause, Actor: sm, On: true})
	h.now += 10_000
	h.do(Command{Type: CmdShowmanPause, Actor: sm, On: false})
	for _, e := range h.ev(EvTimerResume) {
		require.NotEqual(t, TimerButtonPressing, payload[TimerPausePayload](t, e).Kind, "frozen thinking timer must not resume")
	}
	th := h.timer(TimerButtonPressing)
	require.True(t, th.Paused)
	require.Equal(t, int64(3500), th.RemainingMs)
	require.False(t, h.timer(TimerAnswering).Paused)
	h.answer("p1", "no")
	h.validate(false)
	require.Equal(t, int64(3500), payload[TimerPausePayload](t, h.one(EvTimerResume)).RemainingMs)
	require.False(t, h.timer(TimerButtonPressing).Paused)
}

func TestShowmanControls(t *testing.T) {
	h := newH(t, stdPack(), DefaultRules(), "p1", "p2")
	h.fail(Command{Type: CmdMove, Actor: sm, Dir: 1}, ErrBadState)
	h.start("p1")

	// Permissions.
	h.fail(Command{Type: CmdToggle, Actor: player("p1"), Theme: 0, Q: 0}, ErrNotAllowed)
	h.fail(Command{Type: CmdChangeScore, Actor: player("p1"), PersonID: "p1", NewSum: 5}, ErrNotAllowed)
	h.fail(Command{Type: CmdSetChooser, Actor: player("p1"), PersonID: "p1"}, ErrNotAllowed)
	h.fail(Command{Type: CmdMove, Actor: player("p1"), Dir: 1}, ErrNotAllowed)
	h.fail(Command{Type: CmdKickPlayer, Actor: sm, PersonID: "p2"}, ErrNotAllowed)

	// TOGGLE removes and restores.
	h.do(Command{Type: CmdToggle, Actor: sm, Theme: 0, Q: 0})
	require.False(t, payload[TogglePayload](t, h.one(EvToggle)).Active)
	require.True(t, h.st().Table[0].Questions[0].Removed)
	h.fail(Command{Type: CmdChooseQuestion, Actor: player("p1"), Theme: 0, Q: 0}, ErrBadArgument)
	h.do(Command{Type: CmdToggle, Actor: sm, Theme: 0, Q: 0})
	require.False(t, h.st().Table[0].Questions[0].Removed)
	h.fail(Command{Type: CmdToggle, Actor: sm, Theme: 9, Q: 0}, ErrBadArgument)

	// CHANGE (negative allowed) and SETCHOOSER.
	h.setScore("p1", -150)
	require.Equal(t, -150, h.score("p1"))
	require.Equal(t, "correction", payload[PersonScorePayload](t, h.one(EvPersonScore)).Reason)
	h.fail(Command{Type: CmdChangeScore, Actor: sm, PersonID: "zz", NewSum: 1}, ErrBadArgument)
	h.do(Command{Type: CmdSetChooser, Actor: sm, PersonID: "p2"})
	require.Equal(t, "p2", h.st().ChooserID)
	require.Equal(t, "p2", payload[AskChoosePayload](t, h.one(EvAskChoose)).PersonID)

	// MOVE -1 returns the question to the table and undoes its scoring.
	h.choose(0, 1)
	h.runContent()
	h.win("p1")
	h.answer("p1", "x")
	h.validate(false)
	require.Equal(t, -350, h.score("p1"))
	h.do(Command{Type: CmdMove, Actor: sm, Dir: -1})
	require.Equal(t, -150, h.score("p1"))
	require.False(t, h.st().Table[0].Questions[1].Played)
	require.Equal(t, StageSelecting, h.st().Stage)
	h.one(EvButtonDisarm)

	// MOVE 1 during a question reveals the answer.
	h.choose(0, 1)
	h.do(Command{Type: CmdMove, Actor: sm, Dir: 1})
	h.one(EvRightAnswer)
	h.do(Command{Type: CmdMove, Actor: sm, Dir: 1})
	h.one(EvQuestionEnd)
	require.Equal(t, StageSelecting, h.st().Stage)

	// MOVE 2 ends the round manually → final round starts.
	h.do(Command{Type: CmdMove, Actor: sm, Dir: 2})
	require.Equal(t, "manual", payload[RoundEndPayload](t, h.one(EvRoundEnd)).Reason)
	require.Equal(t, 1, h.st().RoundIndex)

	// MOVE -2 goes back a round; MOVE 3 jumps.
	h.do(Command{Type: CmdMove, Actor: sm, Dir: -2})
	require.Equal(t, 0, h.st().RoundIndex)
	h.fail(Command{Type: CmdMove, Actor: sm, Dir: 3, Round: 7}, ErrBadArgument)
	h.fail(Command{Type: CmdMove, Actor: sm, Dir: 9}, ErrBadArgument)
	h.do(Command{Type: CmdMove, Actor: sm, Dir: 3, Round: 1})
	require.Equal(t, 1, h.st().RoundIndex)
	require.Equal(t, -150, h.score("p1")) // scores survive round jumps

	// SET_OPTIONS by host, broadcast as OPTIONS.
	oral := true
	speed := 10
	h.do(Command{Type: CmdSetOptions, Actor: host, Options: &RulesPatch{Oral: &oral, ReadingSpeed: &speed}})
	o := payload[OptionsPayload](t, h.one(EvOptions))
	require.True(t, o.Rules.Oral)
	require.Equal(t, 10, o.Rules.ReadingSpeed)
	h.fail(Command{Type: CmdSetOptions, Actor: host}, ErrBadArgument)
}

func TestRoundTimeoutAndGameEnd(t *testing.T) {
	pack := mkPack(stdRound("R1"), stdRound("R2"))
	h := newH(t, pack, DefaultRules(), "p1", "p2")
	h.start("p1")
	h.choose(0, 0)
	h.runContent()
	h.timeout(TimerRound) // during a question: round ends after it
	require.True(t, h.st().RoundTimedOut)
	require.Equal(t, StageQuestion, h.st().Stage)
	h.win("p1")
	h.answer("p1", "a1")
	h.validate(true)
	h.reveal()
	require.Equal(t, "timeout", payload[RoundEndPayload](t, h.one(EvRoundEnd)).Reason)
	require.Equal(t, 1, h.st().RoundIndex)
	require.Equal(t, "R2", h.st().RoundName)

	// Round 2: p1 (100) vs p2 (0) → lowest score chooses without a tie.
	require.Equal(t, "p2", h.st().ChooserID)
	h.timeout(TimerRound) // while selecting: ends immediately → game over
	h.one(EvRoundEnd)
	require.Equal(t, StageGameEnd, h.st().Stage)
	require.Equal(t, "p1", payload[WinnerPayload](t, h.one(EvWinner)).PersonID)
	ge := payload[GameEndPayload](t, h.one(EvGameEnd))
	require.Equal(t, "p1", ge.WinnerID)
	require.Equal(t, 1, ge.Statistics.QuestionsPlayed)
	require.Equal(t, 1, ge.Statistics.Players[0].RightAnswers)
	h.fail(Command{Type: CmdChooseQuestion, Actor: player("p2")}, ErrBadState)
}

func TestGameEndTie(t *testing.T) {
	pack := mkPack(mkRound("R1", packs.RoundStandard, mkTheme("T", mkQ(100, packs.QSimple, "a"))))
	h := newH(t, pack, DefaultRules(), "p1", "p2")
	h.start("p1")
	h.choose(0, 0)
	h.runContent()
	h.do(Command{Type: CmdButtonResult, Actor: system, Nobody: true})
	h.reveal()
	require.Equal(t, "empty", payload[RoundEndPayload](t, h.one(EvRoundEnd)).Reason)
	require.Equal(t, "", payload[WinnerPayload](t, h.one(EvWinner)).PersonID)
	require.True(t, h.st().Ended)
}

func TestSequentialModes(t *testing.T) {
	quizPack := mkPack(mkRound("R", packs.RoundStandard,
		mkTheme("T1", mkQ(100, "", "a"), mkQ(200, "", "b")), mkTheme("T2", mkQ(100, "", "c"))))
	t.Run("quiz", func(t *testing.T) {
		r := DefaultRules()
		r.Mode = ModeQuiz
		h := newH(t, quizPack, r, "p1", "p2")
		h.do(Command{Type: CmdStart, Actor: host})
		h.none(EvAskChoose)
		h.none(EvAskSelectPlayer)
		qs := payload[QuestionStartPayload](t, h.one(EvQuestionStart))
		require.Equal(t, packs.QForAll, qs.Type)
		require.True(t, qs.IsDefault)
		require.Equal(t, 0, qs.ThemeIndex)
		h.fail(Command{Type: CmdChooseQuestion, Actor: player("p1"), Theme: 0, Q: 1}, ErrBadState)
		h.runContent()
		h.one(EvFinalThink)
		h.answer("p1", "a")
		h.answer("p2", "zz")
		h.validate(true)
		h.validate(false)
		h.reveal()
		qs = payload[QuestionStartPayload](t, h.one(EvQuestionStart))
		require.Equal(t, 1, qs.QuestionIndex) // next in order, no chooser
		require.Equal(t, 100, h.score("p1"))
		require.Equal(t, -100, h.score("p2"))
	})
	t.Run("simple", func(t *testing.T) {
		r := DefaultRules()
		r.Mode = ModeSimple
		h := newH(t, quizPack, r, "p1", "p2")
		h.do(Command{Type: CmdStart, Actor: host})
		qs := payload[QuestionStartPayload](t, h.one(EvQuestionStart))
		require.Equal(t, packs.QSimple, qs.Type)
		h.runContent()
		h.one(EvButtonArmRequest)
		h.win("p2")
		h.answer("p2", "a")
		h.validate(true)
		h.none(EvSetChooser) // no chooser in sequential modes
		h.reveal()
		require.Equal(t, 1, payload[QuestionStartPayload](t, h.one(EvQuestionStart)).QuestionIndex)
	})
	t.Run("turnTaking", func(t *testing.T) {
		r := DefaultRules()
		r.Mode = ModeTurnTaking
		h := newH(t, quizPack, r, "p1", "p2", "p3")
		h.start("p2")
		h.choose(0, 0)
		qs := payload[QuestionStartPayload](t, h.one(EvQuestionStart))
		require.Equal(t, packs.QNoRisk, qs.Type)
		require.True(t, qs.IsDefault)
		h.runContent()
		require.Equal(t, "p2", payload[AskAnswerPayload](t, h.one(EvAskAnswer)).PersonID)
		h.answer("p2", "a")
		h.validate(true)
		require.Equal(t, 200, h.score("p2")) // ×2
		h.reveal()
		sc := payload[SetChooserPayload](t, h.one(EvSetChooser))
		require.Equal(t, "p3", sc.PersonID)
		require.Equal(t, "rotation", sc.Reason)
	})
}

func TestDeterminism(t *testing.T) {
	pack := stdPack()
	run := func() string {
		h := newHSeed(t, pack, DefaultRules(), 7, "p1", "p2", "p3")
		h.start("p1")
		h.timeout(TimerQuestionSelection)
		h.runContent()
		h.timeout(TimerButtonPressing)
		h.reveal()
		h.timeout(TimerQuestionSelection)
		b, err := json.Marshal(h.log)
		require.NoError(t, err)
		return string(b)
	}
	require.Equal(t, run(), run())
	require.Contains(t, run(), `"type":"QUESTION_START"`)
}

func TestManagedModeNext(t *testing.T) {
	r := DefaultRules()
	r.Managed = true
	h := newH(t, stdPack(), r, "p1", "p2")
	h.do(Command{Type: CmdStart, Actor: host})
	require.Equal(t, StageRoundIntro, h.st().Stage)
	require.True(t, h.st().WaitNext)
	h.fail(Command{Type: CmdNext, Actor: player("p1")}, ErrNotAllowed)
	h.do(Command{Type: CmdNext, Actor: sm})
	h.do(Command{Type: CmdSelectPlayer, Actor: sm, PersonID: "p1"})
	h.choose(0, 0)
	h.one(EvContent)
	h.none(EvTimerStart)
	require.True(t, h.q().WaitNext)
	h.do(Command{Type: CmdNext, Actor: sm})
	h.one(EvButtonArmRequest)
	h.win("p1")
	h.answer("p1", "a1")
	h.validate(true)
	h.one(EvRightAnswer)
	require.Nil(t, h.timer(TimerReflection))
	h.do(Command{Type: CmdNext, Actor: sm})
	h.one(EvQuestionEnd)
	h.fail(Command{Type: CmdNext, Actor: sm}, ErrBadState)
}
