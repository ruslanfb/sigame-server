package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sigame/internal/packs"
)

// goToFinal sets scores and jumps to the final round of stdPack.
func goToFinal(t *testing.T, r Rules, scores map[string]int, players ...string) *harness {
	t.Helper()
	h := newH(t, stdPack(), r, players...)
	h.start(players[0])
	for id, s := range scores {
		h.setScore(id, s)
	}
	h.do(Command{Type: CmdMove, Actor: sm, Dir: 2})
	require.Equal(t, packs.RoundFinal, h.st().RoundType)
	return h
}

func TestFinalRound(t *testing.T) {
	h := goToFinal(t, DefaultRules(), map[string]int{"p1": 500, "p2": 1000, "p3": 0}, "p1", "p2", "p3")
	// p3 (≤0) is out.
	require.False(t, h.p("p3").FinalInGame)
	require.Equal(t, PStatePass, h.p("p3").State)
	require.Equal(t, StageFinalThemes, h.st().Stage)
	// Lowest deletes first, highest last.
	ask := payload[AskDeleteThemePayload](t, h.one(EvAskDeleteTheme))
	require.Equal(t, "p1", ask.PersonID)
	require.Equal(t, []int{0, 1, 2}, ask.Themes)
	h.timerStart(TimerThemeSelection)
	h.fail(Command{Type: CmdDeleteTheme, Actor: player("p2"), Theme: 0}, ErrNotAllowed)
	h.do(Command{Type: CmdDeleteTheme, Actor: player("p1"), Theme: 0})
	require.Equal(t, 0, payload[ThemeDeletedPayload](t, h.one(EvThemeDeleted)).ThemeIndex)
	require.True(t, h.st().Table[0].Removed)
	ask = payload[AskDeleteThemePayload](t, h.one(EvAskDeleteTheme))
	require.Equal(t, "p2", ask.PersonID)
	h.fail(Command{Type: CmdDeleteTheme, Actor: player("p2"), Theme: 0}, ErrBadArgument)
	h.do(Command{Type: CmdDeleteTheme, Actor: player("p2"), Theme: 2})

	// Last theme → stakeAll question with hidden stakes.
	require.Equal(t, "F2", payload[QuestionCaptionPayload](t, h.one(EvQuestionCaption)).Theme)
	qs := payload[QuestionStartPayload](t, h.one(EvQuestionStart))
	require.Equal(t, packs.QStakeAll, qs.Type)
	require.True(t, qs.IsDefault)
	require.Equal(t, SubStakes, h.q().Sub)
	require.Len(t, h.ev(EvAskStake), 2)
	a := payload[AskStakePayload](t, h.ev(EvAskStake)[0])
	require.Equal(t, "hiddenStake", a.Reason)
	require.Equal(t, 1, a.Min)
	require.Equal(t, 500, a.Max)
	h.fail(Command{Type: CmdSetStake, Actor: player("p3"), StakeMode: StakeStake, Amount: 1}, ErrNotAllowed)
	h.fail(Command{Type: CmdSetStake, Actor: player("p1"), StakeMode: StakeStake, Amount: 600}, ErrBadArgument)
	h.stake("p1", StakeAllIn, 0)
	// Hidden: everyone but the showman sees no amount.
	for _, e := range h.ev(EvPersonStake) {
		p := payload[PersonStakePayload](t, e)
		require.True(t, p.Hidden)
		if e.Audience == ToShowman() {
			require.Equal(t, 500, p.Amount)
		} else {
			require.Equal(t, ToExcept("sm"), e.Audience)
			require.Equal(t, 0, p.Amount)
		}
	}
	h.stake("p2", StakeStake, 300)
	require.Equal(t, SubContent, h.q().Sub)
	h.runContent()
	h.one(EvFinalThink)
	h.answer("p1", "wrong")
	h.answer("p2", "f2")
	// Sequential reveal in seat order.
	require.Equal(t, "p1", payload[PlayerAnswerPayload](t, h.oneAll(EvPlayerAnswer)).PersonID)
	require.Equal(t, "p1", payload[AskValidatePayload](t, h.lastOf(EvAskValidate)).PersonID)
	h.validate(false)
	ps := payload[PersonStakePayload](t, h.one(EvPersonStake))
	require.Equal(t, 500, ps.Amount)
	require.False(t, ps.Hidden)
	require.Equal(t, 0, h.score("p1")) // wrong all-in → 0
	require.Equal(t, "p2", payload[AskValidatePayload](t, h.one(EvAskValidate)).PersonID)
	h.validate(true)
	require.Equal(t, 1300, h.score("p2"))
	h.one(EvRightAnswer)
	h.reveal()
	h.one(EvQuestionEnd)
	h.one(EvRoundEnd)
	require.Equal(t, "p2", payload[WinnerPayload](t, h.one(EvWinner)).PersonID)
	require.Equal(t, StageGameEnd, h.st().Stage)
}

func TestFinalEligibilityAndTies(t *testing.T) {
	t.Run("nobody positive, everyone allowed", func(t *testing.T) {
		h := goToFinal(t, DefaultRules(), map[string]int{"p1": 0, "p2": -100}, "p1", "p2")
		require.True(t, h.p("p1").FinalInGame)
		require.True(t, h.p("p2").FinalInGame)
		require.Equal(t, StageFinalThemes, h.st().Stage)
		// Tie group → showman picks the deleter.
		// p1=0, p2=-100: different scores, no tie. p2 deletes first.
		require.Equal(t, "p2", payload[AskDeleteThemePayload](t, h.one(EvAskDeleteTheme)).PersonID)
		h.timeout(TimerThemeSelection) // random deletion
		h.one(EvThemeDeleted)
		h.do(Command{Type: CmdDeleteTheme, Actor: player("p1"), Theme: h.finalRemaining()[0]})
		// Score ≤ 1 → automatic stake 1, no prompt; straight to content.
		h.none(EvAskStake)
		require.Equal(t, 1, h.p("p1").Stake)
		require.Equal(t, 1, h.p("p2").Stake)
		require.Equal(t, SubContent, h.q().Sub)
		h.runContent()
		h.timeout(TimerHiddenAnswering)
		require.Equal(t, -1, h.score("p1"))
		require.Equal(t, -101, h.score("p2"))
	})
	t.Run("nobody positive, not allowed → skipped", func(t *testing.T) {
		r := DefaultRules()
		r.AllowEveryoneToPlayHiddenStakes = false
		h := goToFinal(t, r, map[string]int{"p1": 0, "p2": 0}, "p1", "p2")
		require.Equal(t, "noPlayers", payload[RoundEndPayload](t, h.lastOf(EvRoundEnd)).Reason)
		require.Equal(t, StageGameEnd, h.st().Stage)
	})
	t.Run("tie → showman picks deleter; cycling order", func(t *testing.T) {
		h := goToFinal(t, DefaultRules(), map[string]int{"p1": 300, "p2": 300, "p3": 900}, "p1", "p2", "p3")
		// 3 themes, 2 deletions, 3 players: offset 1 → slots [p3-group? no]: groups [[p1,p2],[p3]], slots [0,0,1],
		// step0 → slot 1 → tie group → showman; step1 → slot 2 → p3 (highest last).
		sel := payload[AskSelectPlayerPayload](t, h.one(EvAskSelectPlayer))
		require.Equal(t, "deleter", sel.Reason)
		require.ElementsMatch(t, []string{"p1", "p2"}, sel.Candidates)
		h.do(Command{Type: CmdSelectPlayer, Actor: sm, PersonID: "p2"})
		require.Equal(t, "p2", payload[AskDeleteThemePayload](t, h.one(EvAskDeleteTheme)).PersonID)
		h.do(Command{Type: CmdDeleteTheme, Actor: player("p2"), Theme: 1})
		require.Equal(t, "p3", payload[AskDeleteThemePayload](t, h.one(EvAskDeleteTheme)).PersonID)
	})
	t.Run("play all questions", func(t *testing.T) {
		r := DefaultRules()
		r.PlayAllQuestionsInFinal = true
		h := goToFinal(t, r, map[string]int{"p1": 100, "p2": 100}, "p1", "p2")
		h.none(EvAskDeleteTheme)
		require.Equal(t, 0, payload[QuestionStartPayload](t, h.one(EvQuestionStart)).ThemeIndex)
		h.stake("p1", StakeStake, 50)
		h.stake("p2", StakeStake, 50)
		h.runContent()
		h.timeout(TimerHiddenAnswering)
		h.reveal()
		require.Equal(t, 1, payload[QuestionStartPayload](t, h.one(EvQuestionStart)).ThemeIndex)
	})
}

func (h *harness) finalRemaining() []int { return h.g.finalRemainingThemes() }

// playButtonQuestion: p1 answers wrong, then right answers rightID.
func playWrongThenRight(h *harness, wrongID, rightID string) {
	h.t.Helper()
	h.choose(0, 0)
	h.runContent()
	h.win(wrongID)
	h.answer(wrongID, "wrong")
	h.validate(false)
	h.win(rightID)
	h.answer(rightID, "a1")
	h.validate(true)
	h.reveal()
}

func TestAppealForAccepted(t *testing.T) {
	h := newH(t, stdPack(), DefaultRules(), "p1", "p2", "p3", "p4")
	h.start("p3")
	h.fail(Command{Type: CmdAppellate, Actor: player("p1"), For: true}, ErrBadState)
	playWrongThenRight(h, "p1", "p2")
	require.Equal(t, -100, h.score("p1"))
	require.Equal(t, 100, h.score("p2"))
	require.Equal(t, "p2", h.st().ChooserID)
	require.Equal(t, StageSelecting, h.st().Stage)

	h.fail(Command{Type: CmdAppellate, Actor: player("p3"), For: true}, ErrNotAllowed) // p3 did not answer
	h.do(Command{Type: CmdAppellate, Actor: player("p1"), For: true})
	as := payload[AppealStartPayload](t, h.one(EvAppealStart))
	require.Equal(t, "for", as.Kind)
	require.Equal(t, "wrong", as.Answer)
	require.ElementsMatch(t, []string{"p1", "p2", "p3", "p4", "sm"}, as.Voters)
	require.Len(t, h.ev(EvAskAppealVote), 3) // p2, p3, p4 (p1 and showman pre-filled)
	h.one(EvTimerPause)                      // selection timer frozen
	h.timerStart(TimerAppellation)
	require.NotNil(t, h.st().Appeal)
	h.fail(Command{Type: CmdAppellate, Actor: player("p1"), For: true}, ErrNotAllowed) // once per question
	h.fail(Command{Type: CmdVoteAppeal, Actor: player("p1"), Right: true}, ErrNotAllowed)

	h.do(Command{Type: CmdVoteAppeal, Actor: player("p3"), Right: true})
	h.one(EvAppealVote)
	require.NotNil(t, h.st().Appeal)
	h.do(Command{Type: CmdVoteAppeal, Actor: player("p4"), Right: true}) // 3 of 5 → decided early
	res := payload[AppealResultPayload](t, h.one(EvAppealResult))
	require.True(t, res.Accepted)
	require.Equal(t, 3, res.For)
	require.Equal(t, 1, res.Against)
	require.Equal(t, 100, h.score("p1")) // −100 undone, +100 granted
	require.Equal(t, 0, h.score("p2"))   // later outcome reverted
	sc := payload[SetChooserPayload](t, h.one(EvSetChooser))
	require.Equal(t, "p1", sc.PersonID)
	require.Equal(t, "appeal", sc.Reason)
	require.Equal(t, "p1", payload[AskChoosePayload](t, h.one(EvAskChoose)).PersonID)
	require.Nil(t, h.st().Appeal)
	require.Equal(t, 1, h.p("p1").Right)
	require.Equal(t, 0, h.p("p2").Right)
}

func TestAppealAgainst(t *testing.T) {
	// Too few players.
	h := newH(t, stdPack(), DefaultRules(), "p1", "p2", "p3")
	h.start("p3")
	playWrongThenRight(h, "p1", "p2")
	h.fail(Command{Type: CmdAppellate, Actor: player("p3"), For: false}, ErrNotAllowed)

	// Four players: rejected by timeout with the pre-filled majority.
	h = newH(t, stdPack(), DefaultRules(), "p1", "p2", "p3", "p4")
	h.start("p3")
	playWrongThenRight(h, "p1", "p2")
	h.fail(Command{Type: CmdAppellate, Actor: player("p2"), For: false}, ErrNotAllowed) // own answer
	h.do(Command{Type: CmdAppellate, Actor: player("p3"), For: false})
	as := payload[AppealStartPayload](t, h.one(EvAppealStart))
	require.Equal(t, "against", as.Kind)
	require.Equal(t, "p2", as.AnswererID)
	require.Len(t, h.ev(EvAskAppealVote), 2)                                            // p1, p4
	h.fail(Command{Type: CmdAppellate, Actor: player("p4"), For: false}, ErrNotAllowed) // one "against" per question
	h.do(Command{Type: CmdVoteAppeal, Actor: player("p1"), Right: false})
	h.timeout(TimerAppellation)
	res := payload[AppealResultPayload](t, h.one(EvAppealResult))
	require.False(t, res.Accepted) // for: sm,p2 = 2; against: p3,p1 = 2 → not strictly greater
	require.Equal(t, 100, h.score("p2"))
	h.one(EvTimerResume)

	// Accepted "against": right undone, wrong applied.
	h.choose(1, 0)
	h.runContent()
	h.win("p4")
	h.answer("p4", "b1")
	h.validate(true)
	require.Equal(t, 100, h.score("p4"))
	// Appeal requested during the reveal is queued until the question ends.
	h.do(Command{Type: CmdAppellate, Actor: player("p1"), For: false})
	h.none(EvAppealStart)
	h.reveal()
	h.one(EvQuestionEnd)
	h.one(EvAppealStart)
	h.do(Command{Type: CmdVoteAppeal, Actor: player("p2"), Right: false})
	h.do(Command{Type: CmdVoteAppeal, Actor: player("p3"), Right: false})
	res = payload[AppealResultPayload](t, h.one(EvAppealResult))
	require.True(t, res.Accepted)
	require.Equal(t, -100, h.score("p4"))
	require.Equal(t, StageSelecting, h.st().Stage) // game continued after the appeal
	h.one(EvAskChoose)
}

func TestAppealDisabledAndHidden(t *testing.T) {
	r := DefaultRules()
	r.UseAppellations = false
	h := newH(t, stdPack(), r, "p1", "p2", "p3", "p4")
	h.start("p3")
	playWrongThenRight(h, "p1", "p2")
	h.fail(Command{Type: CmdAppellate, Actor: player("p1"), For: true}, ErrNotAllowed)

	// stakeAll: "for" flips ±stake for that player only.
	h2 := goToFinal(t, DefaultRules(), map[string]int{"p1": 500, "p2": 500}, "p1", "p2")
	h2.do(Command{Type: CmdSelectPlayer, Actor: sm, PersonID: "p1"})
	h2.do(Command{Type: CmdDeleteTheme, Actor: player("p1"), Theme: 0})
	h2.do(Command{Type: CmdDeleteTheme, Actor: player("p2"), Theme: 1})
	h2.stake("p1", StakeStake, 200)
	h2.stake("p2", StakeStake, 100)
	h2.runContent()
	h2.answer("p1", "x")
	h2.answer("p2", "y")
	h2.validate(false)
	h2.validate(false)
	require.Equal(t, 300, h2.score("p1"))
	require.Equal(t, 400, h2.score("p2"))
	h2.fail(Command{Type: CmdAppellate, Actor: player("p2"), For: false}, ErrNotAllowed) // hidden question
	h2.do(Command{Type: CmdAppellate, Actor: player("p1"), For: true})
	h2.none(EvAppealStart) // question still in reveal
	h2.reveal()
	h2.one(EvAppealStart)
	require.Equal(t, StageQuestion, h2.st().Stage)
	h2.do(Command{Type: CmdVoteAppeal, Actor: player("p2"), Right: true})
	require.True(t, payload[AppealResultPayload](t, h2.one(EvAppealResult)).Accepted)
	require.Equal(t, 700, h2.score("p1")) // +200 back, +200 won
	require.Equal(t, 400, h2.score("p2")) // untouched
	h2.one(EvRoundEnd)                    // game continued after the appeal
}
