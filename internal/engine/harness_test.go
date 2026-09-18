package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"sigame/internal/packs"
)

var (
	sm     = Actor{PersonID: "sm", Role: RoleShowman}
	host   = Actor{PersonID: "sm", Role: RoleShowman, IsHost: true}
	system = Actor{Role: RoleSystem}
)

func player(id string) Actor { return Actor{PersonID: id, Role: RolePlayer} }

var qSeq int

func mkQ(price int, typ packs.QuestionType, right string) packs.Question {
	qSeq++
	return packs.Question{ID: fmt.Sprintf("q%d", qSeq), Price: price, Type: typ, Right: []string{right},
		Params: packs.QuestionParams{Question: []packs.ContentItem{{Type: packs.ContentText, Text: "Question text?"}}}}
}

func mkTheme(name string, qs ...packs.Question) packs.Theme {
	return packs.Theme{ID: name, Name: name, Questions: qs}
}

func mkRound(name string, typ packs.RoundType, themes ...packs.Theme) packs.Round {
	return packs.Round{ID: name, Name: name, Type: typ, Themes: themes}
}

func mkPack(rounds ...packs.Round) *packs.Pack {
	return &packs.Pack{ID: "pack", Name: "Test", Rounds: rounds}
}

// stdRound is 2 themes × 3 simple questions (100/200/300).
func stdRound(name string) packs.Round {
	return mkRound(name, packs.RoundStandard,
		mkTheme("T1", mkQ(100, packs.QSimple, "a1"), mkQ(200, packs.QSimple, "a2"), mkQ(300, packs.QSimple, "a3")),
		mkTheme("T2", mkQ(100, packs.QSimple, "b1"), mkQ(200, packs.QSimple, "b2"), mkQ(300, packs.QSimple, "b3")),
	)
}

func finalRound() packs.Round {
	return mkRound("Final", packs.RoundFinal,
		mkTheme("F1", mkQ(0, "", "f1")), mkTheme("F2", mkQ(0, "", "f2")), mkTheme("F3", mkQ(0, "", "f3")))
}

// stdPack: one standard round + a final round.
func stdPack() *packs.Pack { return mkPack(stdRound("R1"), finalRound()) }

type harness struct {
	t    *testing.T
	g    *Game
	now  int64
	last []Event
	log  []Event
}

func newH(t *testing.T, pack *packs.Pack, rules Rules, players ...string) *harness {
	t.Helper()
	return newHSeed(t, pack, rules, 42, players...)
}

func newHSeed(t *testing.T, pack *packs.Pack, rules Rules, seed int64, players ...string) *harness {
	t.Helper()
	pis := make([]PlayerInit, len(players))
	for i, id := range players {
		pis[i] = PlayerInit{ID: id, Name: id}
	}
	g, err := New(pack, rules, DefaultTimeSettings(), pis, "sm", seed)
	require.NoError(t, err)
	return &harness{t: t, g: g}
}

func (h *harness) do(cmd Command) []Event {
	h.t.Helper()
	cmd.AtMs = h.now
	evs, err := h.g.Apply(cmd)
	require.NoError(h.t, err, "cmd %s", cmd.Type)
	h.last = evs
	h.log = append(h.log, evs...)
	return evs
}

func (h *harness) fail(cmd Command, want error) {
	h.t.Helper()
	cmd.AtMs = h.now
	_, err := h.g.Apply(cmd)
	require.ErrorIs(h.t, err, want, "cmd %s", cmd.Type)
}

func (h *harness) st() *State            { return h.g.State() }
func (h *harness) q() *QuestionState     { return h.g.State().Question }
func (h *harness) p(id string) *Player   { return h.g.State().player(id) }
func (h *harness) score(id string) int   { return h.p(id).Score }
func (h *harness) timer(k string) *Timer { return h.g.timerOfKind(k) }

// timeout fires the timer of the given kind at its deadline.
func (h *harness) timeout(kind string) []Event {
	h.t.Helper()
	t := h.timer(kind)
	require.NotNil(h.t, t, "no timer of kind %s", kind)
	if rem := t.DurationMs - (h.now - t.StartedAtMs); rem > 0 {
		h.now += rem
	}
	return h.do(Command{Type: CmdTimeout, Actor: system, TimerID: t.ID})
}

func evs(list []Event, t EvType) []Event {
	var out []Event
	for _, e := range list {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

func (h *harness) ev(t EvType) []Event { return evs(h.last, t) }
func (h *harness) has(t EvType) bool   { return len(h.ev(t)) > 0 }

func (h *harness) one(t EvType) Event {
	h.t.Helper()
	l := h.ev(t)
	require.NotEmpty(h.t, l, "expected event %s in %v", t, types(h.last))
	return l[0]
}

// oneAll returns the first event of type t addressed to everyone.
func (h *harness) oneAll(t EvType) Event {
	h.t.Helper()
	for _, e := range h.ev(t) {
		if e.Audience == ToAll() {
			return e
		}
	}
	require.Failf(h.t, "missing event", "no %s to all in %v", t, types(h.last))
	return Event{}
}

// lastOf returns the last event of type t.
func (h *harness) lastOf(t EvType) Event {
	h.t.Helper()
	l := h.ev(t)
	require.NotEmpty(h.t, l, "expected event %s in %v", t, types(h.last))
	return l[len(l)-1]
}

func (h *harness) none(t EvType) {
	h.t.Helper()
	require.Empty(h.t, h.ev(t), "unexpected event %s", t)
}

func types(list []Event) []EvType {
	out := make([]EvType, len(list))
	for i, e := range list {
		out[i] = e.Type
	}
	return out
}

func payload[T any](t *testing.T, e Event) T {
	t.Helper()
	v, ok := e.Payload.(T)
	require.True(t, ok, "payload of %s is %T", e.Type, e.Payload)
	return v
}

func (h *harness) timerStart(kind string) TimerStartPayload {
	h.t.Helper()
	for _, e := range h.ev(EvTimerStart) {
		if p := payload[TimerStartPayload](h.t, e); p.Kind == kind {
			return p
		}
	}
	require.Failf(h.t, "missing timer", "no TIMER_START of kind %s in %v", kind, types(h.last))
	return TimerStartPayload{}
}

// ---- flow helpers ----------------------------------------------------------

// start starts the game; in modes with a chooser the showman resolves the
// all-zero tie by picking chooser.
func (h *harness) start(chooser string) {
	h.t.Helper()
	h.do(Command{Type: CmdStart, Actor: host})
	if h.st().Pending != nil && h.st().Pending.Kind == PendingChooser {
		h.do(Command{Type: CmdSelectPlayer, Actor: sm, PersonID: chooser})
	}
}

func (h *harness) choose(theme, q int) {
	h.t.Helper()
	h.do(Command{Type: CmdChooseQuestion, Actor: player(h.st().ChooserID), Theme: theme, Q: q})
}

// runContent fires content timers until the content phase is over.
func (h *harness) runContent() {
	h.t.Helper()
	for i := 0; i < 50 && h.q() != nil && h.q().Sub == SubContent; i++ {
		switch {
		case h.timer(TimerContent) != nil:
			h.timeout(TimerContent)
		case h.timer(TimerMediaFallback) != nil:
			h.timeout(TimerMediaFallback)
		default:
			return
		}
	}
}

func (h *harness) win(id string) []Event {
	h.t.Helper()
	return h.do(Command{Type: CmdButtonResult, Actor: system, WinnerID: id})
}

func (h *harness) answer(id, text string) []Event {
	h.t.Helper()
	return h.do(Command{Type: CmdAnswer, Actor: player(id), Text: text})
}

func (h *harness) validate(right bool) []Event {
	h.t.Helper()
	return h.do(Command{Type: CmdValidate, Actor: sm, Right: right})
}

func (h *harness) reveal() []Event {
	h.t.Helper()
	return h.timeout(TimerReflection)
}

func (h *harness) setScore(id string, sum int) {
	h.t.Helper()
	h.do(Command{Type: CmdChangeScore, Actor: sm, PersonID: id, NewSum: sum})
}

func (h *harness) stake(id string, mode StakeMode, amount int) []Event {
	h.t.Helper()
	return h.do(Command{Type: CmdSetStake, Actor: player(id), StakeMode: mode, Amount: amount})
}
