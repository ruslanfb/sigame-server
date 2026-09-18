package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"sigame/internal/packs"
)

func TestProjectionNeverLeaksAnswers(t *testing.T) {
	q := mkQ(100, packs.QSimple, "SECRET_ANSWER")
	q.Info.ShowmanComments = "SHOWMAN_ONLY"
	q.Wrong = []string{"KNOWN_WRONG"}
	pack := mkPack(mkRound("R", packs.RoundStandard, mkTheme("T1", q)))
	r := DefaultRules()
	r.HintShowman = true
	h := newH(t, pack, r, "p1", "p2")
	h.start("p1")
	h.choose(0, 0)
	h.one(EvShowmanHint)
	h.runContent()
	h.win("p1")
	h.answer("p1", "guess")
	// Everything up to here must not contain the answer outside the showman audience.
	for _, e := range h.log {
		if e.Audience == ToShowman() || e.IsInternal() {
			continue
		}
		b, err := json.Marshal(e)
		require.NoError(t, err)
		require.NotContains(t, string(b), "SECRET_ANSWER", "event %s leaks the answer", e.Type)
		require.NotContains(t, string(b), "SHOWMAN_ONLY", "event %s leaks showman comments", e.Type)
		require.NotContains(t, string(b), "KNOWN_WRONG", "event %s leaks wrong answers", e.Type)
	}
	// Snapshot projection.
	snapP := h.g.Snapshot(RolePlayer, "p2")
	require.Nil(t, snapP.Question.Rights)
	require.Nil(t, snapP.AskValidate)
	b, _ := json.Marshal(snapP)
	require.NotContains(t, string(b), "SECRET_ANSWER")
	snapS := h.g.Snapshot(RoleShowman, "sm")
	require.Equal(t, []string{"SECRET_ANSWER"}, snapS.Question.Rights)
	require.NotNil(t, snapS.AskValidate)
	require.Equal(t, "guess", snapS.AskValidate.Answer)
	require.Equal(t, SubValidating, snapS.Sub)
	require.Len(t, snapS.Timers, 3) // round + paused thinking + showmanDecision

	h.validate(true)
	require.NotNil(t, h.g.Snapshot(RoleViewer, "v").Question.RightAnswer)
	// The reveal contains the answer for everyone.
	require.Equal(t, "SECRET_ANSWER", payload[RightAnswerPayload](t, h.one(EvRightAnswer)).Text)
}

func TestSnapshotPromptsAndHiddenData(t *testing.T) {
	h := goToFinal(t, DefaultRules(), map[string]int{"p1": 500, "p2": 800}, "p1", "p2")
	s := h.g.Snapshot(RolePlayer, "p1")
	require.Equal(t, StageFinalThemes, s.Stage)
	require.NotNil(t, s.AskDeleteTheme)
	require.Nil(t, h.g.Snapshot(RolePlayer, "p2").AskDeleteTheme)
	h.do(Command{Type: CmdDeleteTheme, Actor: player("p1"), Theme: 0})
	h.do(Command{Type: CmdDeleteTheme, Actor: player("p2"), Theme: 1})
	s = h.g.Snapshot(RolePlayer, "p1")
	require.NotNil(t, s.AskStake)
	require.Equal(t, 500, s.AskStake.Max)
	h.stake("p1", StakeStake, 200)
	s = h.g.Snapshot(RolePlayer, "p2")
	require.Nil(t, s.Question.HiddenStakes) // p1's stake hidden from p2
	require.NotNil(t, s.AskStake)
	require.Equal(t, 200, h.g.Snapshot(RolePlayer, "p1").Question.HiddenStakes["p1"])
	require.Equal(t, 200, h.g.Snapshot(RoleShowman, "sm").Question.HiddenStakes["p1"])
	h.stake("p2", StakeStake, 100)
	h.runContent()
	s = h.g.Snapshot(RolePlayer, "p2")
	require.NotNil(t, s.AskAnswer)
	require.True(t, s.AskAnswer.Hidden)
	h.answer("p1", "mine")
	require.Nil(t, h.g.Snapshot(RolePlayer, "p2").Question.HiddenAnswers)
	require.Equal(t, "mine", h.g.Snapshot(RolePlayer, "p1").Question.HiddenAnswers["p1"].Text)
	require.Equal(t, "mine", h.g.Snapshot(RoleShowman, "sm").Question.HiddenAnswers["p1"].Text)
	require.Nil(t, h.g.Snapshot(RolePlayer, "p1").AskAnswer)

	// Selecting-stage snapshot for the chooser; stake bidding snapshot.
	h2 := newH(t, mkPack(mkRound("R", packs.RoundStandard, mkTheme("T", mkQ(100, packs.QStake, "s"), mkQ(100, packs.QStake, "s")))), DefaultRules(), "p1", "p2")
	h2.start("p1")
	require.NotNil(t, h2.g.Snapshot(RolePlayer, "p1").AskChoose)
	require.Nil(t, h2.g.Snapshot(RolePlayer, "p2").AskChoose)
	h2.setScore("p2", 300)
	h2.choose(0, 0)
	require.Equal(t, 100, h2.q().Stakes.Stake) // p1 forced nominal
	s = h2.g.Snapshot(RolePlayer, "p2")
	require.NotNil(t, s.AskStake)
	require.Equal(t, "stake", s.AskStake.Reason)
	require.Equal(t, 100, s.Question.Stakes.Bids["p1"])
	b, err := json.Marshal(s)
	require.NoError(t, err)
	require.True(t, strings.Contains(string(b), `"askStake"`))
}

func TestPlayersJoinLeaveKick(t *testing.T) {
	h := newH(t, stdPack(), DefaultRules(), "p1", "p2", "p3")
	h.start("p1")
	h.fail(Command{Type: CmdPlayerLeft, Actor: player("p1"), PersonID: "p1"}, ErrNotAllowed)
	h.fail(Command{Type: CmdPlayerJoined, Actor: system, PersonID: "sm"}, ErrBadArgument)

	// New player joins mid-game.
	h.do(Command{Type: CmdPlayerJoined, Actor: system, PersonID: "p4", Name: "Four"})
	require.Len(t, payload[PlayersPayload](t, h.one(EvPlayers)).Players, 4)
	h.one(EvSums)

	// forAll: a player leaving completes the hidden phase.
	h.g.st.Rules.Mode = ModeClassic
	pack := mkPack(mkRound("R", packs.RoundStandard, mkTheme("T", mkQ(100, packs.QForAll, "fa"))))
	h = newH(t, pack, DefaultRules(), "p1", "p2")
	h.start("p1")
	h.choose(0, 0)
	h.runContent()
	h.answer("p1", "fa")
	h.do(Command{Type: CmdPlayerLeft, Actor: system, PersonID: "p2"})
	require.False(t, h.p("p2").Connected)
	h.one(EvPlayerAnswer) // reveal started
	require.Equal(t, SubValidating, h.q().Sub)
	h.validate(true)
	require.Equal(t, 100, h.score("p1"))
	require.Equal(t, 0, h.score("p2")) // skipped, no penalty
	// Reconnect keeps the score; kicked players cannot return.
	h.do(Command{Type: CmdPlayerJoined, Actor: system, PersonID: "p2"})
	require.True(t, h.p("p2").Connected)
	h.fail(Command{Type: CmdKickPlayer, Actor: host, PersonID: "zz"}, ErrBadArgument)
	h.do(Command{Type: CmdKickPlayer, Actor: host, PersonID: "p2"})
	require.True(t, h.p("p2").Kicked)
	h.fail(Command{Type: CmdPlayerJoined, Actor: system, PersonID: "p2"}, ErrNotAllowed)
	require.Len(t, h.g.scores(), 1)
}

func TestStakeAllOutsideFinal(t *testing.T) {
	pack := mkPack(mkRound("R", packs.RoundStandard, mkTheme("T", mkQ(100, packs.QStakeAll, "sa"))))
	r := DefaultRules()
	r.AllowEveryoneToPlayHiddenStakes = false
	h := newH(t, pack, r, "p1", "p2", "p3")
	h.start("p1")
	h.setScore("p2", 50)
	h.choose(0, 0)
	// Only p2 (>0) plays; p2 with 50 is asked for a stake.
	require.Equal(t, []string{"p2"}, h.q().Answerers)
	require.Len(t, h.ev(EvAskStake), 1)
	h.timeout(TimerStakeMaking) // → 1
	require.Equal(t, 1, h.p("p2").Stake)
	h.runContent()
	h.fail(Command{Type: CmdAnswer, Actor: player("p1"), Text: "x"}, ErrNotAllowed)
	h.answer("p2", "sa")
	h.validate(true)
	require.Equal(t, 51, h.score("p2"))
}

func TestAISuggestionAndMisc(t *testing.T) {
	h := newH(t, stdPack(), DefaultRules(), "p1", "p2")
	h.start("p1")
	h.fail(Command{Type: CmdAISuggestion, Actor: player("p1")}, ErrNotAllowed)
	h.do(Command{Type: CmdAISuggestion, Actor: system, PersonID: "p1", Right: true, Reason: "fuzzy"})
	e := h.one(EvAISuggestion)
	require.Equal(t, ToShowman(), e.Audience)
	require.Equal(t, 1.0, payload[AISuggestionPayload](t, e).Factor)
	h.do(Command{Type: CmdMediaLoaded, Actor: player("p1")})
	require.Empty(t, h.last)
	h.fail(Command{Type: "BOGUS", Actor: sm}, ErrBadArgument)
	h.fail(Command{Type: CmdValidate, Actor: sm, Right: true}, ErrBadState)
	h.fail(Command{Type: CmdSetStake, Actor: player("p1")}, ErrBadState)
	h.fail(Command{Type: CmdDeleteTheme, Actor: player("p1")}, ErrBadState)
	h.fail(Command{Type: CmdVoteAppeal, Actor: player("p1")}, ErrBadState)
	h.fail(Command{Type: CmdPass, Actor: player("p1")}, ErrBadState)
	h.fail(Command{Type: CmdAnswerDraft, Actor: player("p1")}, ErrBadState)
	h.fail(Command{Type: CmdSelectPlayer, Actor: sm, PersonID: "p1"}, ErrBadState)
	require.False(t, Event{Type: EvSums, Audience: ToAll()}.IsInternal())
	require.True(t, Event{Type: EvTimerStop}.IsTimer())
	require.True(t, ToRoom().IsRoomOnly())

	// Defaults are sane and JSON-friendly.
	b, err := json.Marshal(OptionsPayload{Rules: DefaultRules(), Times: DefaultTimeSettings()})
	require.NoError(t, err)
	require.Contains(t, string(b), `"readingSpeed":20`)
	require.Contains(t, string(b), `"buttonPressingMs":5000`)

	// Text with reading speed 0 waits for Next even outside Managed mode.
	speed := 0
	h.do(Command{Type: CmdSetOptions, Actor: sm, Options: &RulesPatch{ReadingSpeed: &speed}})
	h.choose(0, 0)
	require.True(t, h.q().WaitNext)
	h.do(Command{Type: CmdNext, Actor: sm})
	h.one(EvButtonArmRequest)
}

func TestThemeCommentsAndAnswerDuration(t *testing.T) {
	q := mkQ(100, packs.QNoRisk, "x")
	q.Params.AnswerDurationMs = 7000
	q.Params.Answer = []packs.ContentItem{{Type: packs.ContentImage, MediaID: "ans"}}
	th := mkTheme("T", q)
	th.Info.Comments = "Theme comment"
	pack := mkPack(mkRound("R", packs.RoundStandard, th))
	h := newH(t, pack, DefaultRules(), "p1")
	h.start("p1")
	h.choose(0, 0)
	c := payload[ContentPayload](t, h.one(EvContent))
	require.Equal(t, "Theme comment", c.Items[0].Text)
	h.runContent()
	require.Equal(t, int64(7000), h.timerStart(TimerSoloAnswering).DurationMs)
	h.answer("p1", "x")
	h.validate(true)
	ra := payload[RightAnswerPayload](t, h.one(EvRightAnswer))
	require.Equal(t, "ans", ra.Items[0].MediaID)
	require.Equal(t, int64(5000), h.timerStart(TimerReflection).DurationMs) // image time > reflection
}
