package buzzer

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

type fixture struct {
	s       Settings
	players []Player
	conns   map[string]*Conn
	offsets map[string]float64
	rtts    map[string]float64
	plans   map[string]ArmPlan
	now     float64
}

// newFixture syncs one connection per (id, rtt, σ) triple on symmetric links.
func newFixture(t *testing.T, s Settings, specs [][3]float64, ids ...string) *fixture {
	t.Helper()
	rng := rand.New(rand.NewSource(42))
	f := &fixture{s: s, conns: map[string]*Conn{}, offsets: map[string]float64{}, rtts: map[string]float64{}, plans: map[string]ArmPlan{}}
	for i, id := range ids {
		off := float64(1000 * (i + 1))
		c, end := syncedConn(0, specs[i][0], specs[i][1], off, rng)
		f.conns[id], f.offsets[id], f.rtts[id] = c, off, specs[i][0]
		if end > f.now {
			f.now = end
		}
		f.players = append(f.players, Player{ID: id, Conn: c, Eligible: true, Seat: i})
	}
	return f
}

// arm creates an arm lit exactly at tArm and delivers every plan + ack on a symmetric link.
func (f *fixture) arm(t *testing.T, tArm float64, seed string) *Arm {
	t.Helper()
	s := f.s
	s.ArmJitterMinMs, s.ArmJitterMaxMs = 0, 0
	a := NewArm(f.now, tArm, f.players, s, []byte(seed))
	require.Equal(t, tArm, a.TArm())
	for _, p := range a.Plans() {
		f.plans[p.PlayerID] = p
		a.OnSent(p.PlayerID, p.SendAt)
		a.OnArmAck(p.PlayerID, p.SendAt+f.rtts[p.PlayerID], p.SendAt+f.rtts[p.PlayerID]/2-f.offsets[p.PlayerID])
	}
	return a
}

// lightAt returns the server instant at which an honest client lights: armAtLocal on its own clock.
func (f *fixture) lightAt(id string) float64 {
	if p, ok := f.plans[id]; ok && p.Msg.Mode == "scheduled" {
		return p.Msg.ArmAtLocal + f.offsets[id]
	}
	return f.plans[id].Msg.ArmAt
}

// press submits an honest press with reaction (at − T_arm) from the player's own
// light, which sits at armAtLocal on the client clock (≈ T_arm ± offset error).
func (f *fixture) press(a *Arm, id string, at float64) PressAck {
	light := f.lightAt(id)
	pressAt := light + (at - a.TArm())
	return a.OnPress(PressIn{PlayerID: id, Seq: 1, PressLocal: pressAt - f.offsets[id], LitLocal: light - f.offsets[id]}, pressAt+f.rtts[id]/2)
}

func resolveNow(t *testing.T, a *Arm) Result {
	t.Helper()
	for i := 0; i < 3; i++ {
		at, ok := a.NextDeadline()
		require.True(t, ok)
		if res, done := a.Resolve(at); done {
			return res
		}
	}
	require.Fail(t, "arm did not resolve", "state %s", a.State())
	return Result{}
}

// TestWorkedExample is the syntheses' example: RTT 10/80/250, physical presses
// at 1000/990/985 → arrival order A, B, C but C reacted first and must win.
func TestWorkedExample(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode TieMode
		tb   TieBreak
	}{
		{"resolution", TieResolution, TieBreakMostLikely},
		{"uncertainty-mostLikely", TieUncertainty, TieBreakMostLikely},
		{"uncertainty-likelihood", TieUncertainty, TieBreakLikelihood},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := Preset(PresetInternetFair)
			s.TieMode, s.TieBreak = tc.mode, tc.tb
			f := newFixture(t, s, [][3]float64{{10, 0.5}, {80, 5}, {250, 15}}, "A", "B", "C")
			f.now = 400
			a := f.arm(t, 700, "worked")
			require.Equal(t, StateArmed, a.State())
			require.Equal(t, StatusPending, f.press(a, "A", 1000).Status) // tRecv 1005
			require.Equal(t, StateCollecting, a.State())
			require.Equal(t, StatusPending, f.press(a, "B", 990).Status) // tRecv 1030
			require.Equal(t, StatusPending, f.press(a, "C", 985).Status) // tRecv 1110
			at, ok := a.NextDeadline()
			require.True(t, ok)
			require.InDelta(t, 1110, at, 3, "window closes as soon as everyone pressed (± residual offset error)")
			res := resolveNow(t, a)
			require.Equal(t, KindWinner, res.Kind)
			require.Equal(t, "C", res.WinnerID, "rule=%s ranking=%+v", res.Rule, res.Ranking)
			require.Equal(t, []string{"C", "B", "A"}, []string{res.Ranking[0].PlayerID, res.Ranking[1].PlayerID, res.Ranking[2].PlayerID})
			require.InDelta(t, 285, res.Ranking[0].ReactionMs, 1e-6, "reaction from the own light is exact")
			require.InDelta(t, 290, res.Ranking[1].ReactionMs, 1e-6)
			require.InDelta(t, 300, res.Ranking[2].ReactionMs, 1e-6)
			for _, r := range res.Ranking {
				require.Equal(t, SourceClient, r.Source)
			}
			require.Len(t, a.Audit(), 3)
			require.InDelta(t, 105, res.CollectMs, 3) // ± residual offset errors
		})
	}
}

func TestCollectionWindow(t *testing.T) {
	s := DefaultSettings()
	f := newFixture(t, s, [][3]float64{{10, 0.5}, {80, 5}, {250, 15}}, "A", "B", "C")
	f.now = 400
	a := f.arm(t, 700, "window")
	dl, ok := a.NextDeadline()
	require.True(t, ok)
	require.Equal(t, 700+float64(s.PressWindowMs), dl, "armed: press window end")

	require.Equal(t, StatusPending, f.press(a, "A", 1000).Status)
	dl, _ = a.NextDeadline()
	// bestT + tieMax + max_j min(rttRef_j/2 + tol_j, 200) but never beyond firstRecv + maxCollect
	require.Greater(t, dl, 1005.0)
	require.LessOrEqual(t, dl, 1005+float64(s.MaxCollectMs))
	_, done := a.Resolve(1100)
	require.False(t, done, "not due yet")
	require.Equal(t, StatusPending, f.press(a, "B", 990).Status)
	dl2, _ := a.NextDeadline()
	require.Less(t, dl2, dl, "a better press shortens the window")
	// C never presses → the window closes at its deadline, B wins
	res, done := a.Resolve(dl2)
	require.True(t, done)
	require.Equal(t, "B", res.WinnerID)

	// cap: a pending player with a huge RTT cannot hold the window beyond maxCollect
	g := newFixture(t, s, [][3]float64{{10, 0.5}, {180, 25}}, "A", "Z")
	g.now = 400
	b := g.arm(t, 700, "cap")
	require.Equal(t, StatusPending, g.press(b, "A", 1000).Status)
	dl, _ = b.NextDeadline()
	require.LessOrEqual(t, dl, 1005+float64(s.MaxCollectMs))
	// a player who disconnects mid-window stops extending it
	b.SetEligible("Z", false, 1010)
	dl, _ = b.NextDeadline()
	require.Equal(t, 1010.0, dl)
	require.Equal(t, StatusStale, b.OnPress(PressIn{PlayerID: "Z"}, 1011).Status)

	// nobody presses: armed → draining → nobody
	h := newFixture(t, s, [][3]float64{{10, 0.5}}, "A")
	h.now = 400
	e := h.arm(t, 700, "nobody")
	_, done = e.Resolve(700 + float64(s.PressWindowMs))
	require.False(t, done)
	require.Equal(t, StateDraining, e.State())
	dl, _ = e.NextDeadline()
	res, done = e.Resolve(dl)
	require.True(t, done)
	require.Equal(t, KindNobody, res.Kind)
	require.Equal(t, StatusLate, h.press(e, "A", dl+1).Status)
}

func TestPressStatuses(t *testing.T) {
	s := DefaultSettings()
	f := newFixture(t, s, [][3]float64{{10, 0.5}, {80, 5}}, "A", "B")
	f.now = 400
	a := f.arm(t, 700, "statuses")
	require.Equal(t, StatusStale, a.OnPress(PressIn{PlayerID: "nobody"}, 900).Status)
	// press physically before the light → false start + lockout
	ack := f.press(a, "A", 650)
	require.Equal(t, StatusFalseStart, ack.Status)
	require.Greater(t, ack.LockoutUntil, 655.0)
	require.Equal(t, f.conns["A"].LockedUntil(), ack.LockoutUntil)
	require.Equal(t, StatusLockedOut, f.press(a, "A", 900).Status)
	// superhuman reaction → rejected, flagged, locked out
	ack = f.press(a, "B", 750)
	require.Equal(t, StatusTooEarly, ack.Status)
	require.True(t, hasFlag(f.conns["B"], FlagTooEarly))
	require.Equal(t, KindNobody, resolveNow(t, a).Kind)
	require.Len(t, a.Audit(), 2)

	g := newFixture(t, s, [][3]float64{{10, 0.5}, {80, 5}}, "A", "B")
	g.now = 400
	b := g.arm(t, 700, "dupes")
	require.Equal(t, StatusPending, g.press(b, "A", 1000).Status)
	require.Equal(t, StatusDuplicate, g.press(b, "A", 1001).Status)
	res := resolveNow(t, b)
	require.Equal(t, "A", res.WinnerID)
	late := g.press(b, "B", 990)
	require.Equal(t, StatusLate, late.Status)
	require.Contains(t, b.Audit()[len(b.Audit())-1].Flags, FlagLateBeat)
	b.Disarm(2000, "cancelled")
	require.Equal(t, StateResolved, b.State(), "a resolved arm cannot be disarmed")
	d := g.arm(t, 3000, "disarm")
	d.Disarm(3100, "cancelled")
	require.Equal(t, StateDisarmed, d.State())
	require.Equal(t, StatusStale, g.press(d, "A", 3300).Status)
	res, done := d.Resolve(3300)
	require.True(t, done)
	require.Equal(t, KindNobody, res.Kind)

	// rate limit: the 6th PRESS within a second is dropped
	h := newFixture(t, s, [][3]float64{{10, 0.5}}, "A")
	h.now = 400
	c := h.arm(t, 700, "rate")
	for i := 0; i < 5; i++ {
		c.OnPress(PressIn{PlayerID: "A"}, 500+float64(i))
	}
	require.Equal(t, StatusRejected, c.OnPress(PressIn{PlayerID: "A"}, 506).Status)
	require.True(t, hasFlag(h.conns["A"], FlagRateLimit))
}

func TestLightOnReceiptPressRejectedByReactLB(t *testing.T) {
	s := DefaultSettings()
	f := newFixture(t, s, [][3]float64{{10, 0.5}}, "A")
	f.now = 400
	a := f.arm(t, 700, "receipt")
	plan := a.Plans()[0]
	tArrive := plan.SendAt + 5 // ARM reaches the modded client
	// the client lights on receipt and presses immediately, claiming lit == press
	ack := a.OnPress(PressIn{PlayerID: "A", PressLocal: tArrive - 1000, LitLocal: tArrive - 1000}, tArrive+5)
	require.Contains(t, []string{StatusTooEarly, StatusFalseStart}, ack.Status)
	require.NotEqual(t, StatusPending, ack.Status)
	require.Greater(t, ack.LockoutUntil, tArrive)
}

func TestLateLightCreditBounded(t *testing.T) {
	s := DefaultSettings()
	f := newFixture(t, s, [][3]float64{{60, 5}}, "A")
	f.now = 400
	a := f.arm(t, 700, "credit")
	// ARM delivery was 80 ms late: the ack shows it, the light lit 50 ms late.
	plan := a.Plans()[0]
	a.OnArmAck("A", plan.SendAt+60+80, 0) // second ack is ignored; use a fresh arm instead
	b := NewArm(400, 700, f.players, func() Settings { s := s; s.ArmJitterMinMs, s.ArmJitterMaxMs = 0, 0; return s }(), []byte("credit2"))
	p := b.Plans()[0]
	b.OnSent("A", p.SendAt)
	b.OnArmAck("A", p.SendAt+60+80, 0)
	lit := p.Msg.ArmAtLocal + 1000 + 50 // 50 ms late light (server time)
	ack := b.OnPress(PressIn{PlayerID: "A", PressLocal: lit + 250 - 1000, LitLocal: lit - 1000}, lit+250+30)
	require.Equal(t, StatusPending, ack.Status)
	e := b.Audit()[0]
	require.LessOrEqual(t, e.Credit, float64(s.LateLightCreditMs)+1e-9, "credit bounded by lateLightCreditMs")
	require.Greater(t, e.Credit, 0.0)
	require.InDelta(t, 250+50-e.Credit, ack.ReactionMs, 1e-6)
	// tournament preset gives no credit at all
	tt, _ := Preset(PresetTournament)
	tt.ArmJitterMinMs, tt.ArmJitterMaxMs = 0, 0
	c := NewArm(400, 700, f.players, tt, []byte("credit3"))
	c.OnSent("A", c.Plans()[0].SendAt)
	c.OnArmAck("A", c.Plans()[0].SendAt+140, 0)
	c.OnPress(PressIn{PlayerID: "A", PressLocal: lit + 250 - 1000, LitLocal: lit - 1000}, lit+250+30)
	require.Equal(t, 0.0, c.Audit()[0].Credit)
}

func TestTieBreaks(t *testing.T) {
	base := DefaultSettings()
	base.TieMode = TieUncertainty
	mk := func(tb TieBreak, seed string) (*fixture, *Arm) {
		s := base
		s.TieBreak = tb
		f := newFixture(t, s, [][3]float64{{10, 0.5}, {12, 0.5}, {14, 0.5}}, "A", "B", "C")
		f.players[0].Score, f.players[1].Score, f.players[2].Score = 300, 100, 200
		f.players[0].ButtonsWon, f.players[1].ButtonsWon, f.players[2].ButtonsWon = 2, 3, 0
		f.now = 400
		return f, f.arm(t, 700, seed)
	}
	// A and B press 3 ms apart (inside the ~7.7 ms client–client window), C far behind.
	run := func(tb TieBreak, seed string) Result {
		f, a := mk(tb, seed)
		f.press(a, "A", 1000)
		f.press(a, "B", 1003)
		f.press(a, "C", 1100)
		return resolveNow(t, a)
	}
	res := run(TieBreakMostLikely, "s1")
	require.True(t, res.Contested)
	require.Equal(t, "A", res.WinnerID)
	require.ElementsMatch(t, []string{"A", "B"}, res.Tie)
	require.InDelta(t, 3, res.MarginMs, 0.2)
	require.Equal(t, "B", run(TieBreakLowestScore, "s1").WinnerID)
	require.Equal(t, "A", run(TieBreakFewestButtonsWon, "s1").WinnerID)
	all := run(TieBreakAllPlay, "s1")
	require.Equal(t, KindAllPlay, all.Kind)
	require.Empty(t, all.WinnerID)
	require.ElementsMatch(t, []string{"A", "B"}, all.Tie)
	// likelihood: leader wins most seeds, but the lottery is reproducible
	wins := map[string]int{}
	for i := 0; i < 40; i++ {
		wins[run(TieBreakLikelihood, "seed"+string(rune('a'+i))).WinnerID]++
	}
	require.Greater(t, wins["A"], wins["B"])
	require.Greater(t, wins["B"], 0)
	require.Equal(t, run(TieBreakLikelihood, "x").WinnerID, run(TieBreakLikelihood, "x").WinnerID)
	// rotate: after seat 0 (A) the next seat in the tie is B
	f, a := mk(TieBreakRotate, "rot")
	a.SetRotateAfter(0)
	f.press(a, "A", 1000)
	f.press(a, "B", 1003)
	require.Equal(t, "B", resolveNow(t, a).WinnerID)
	// clean-first presumption: a flagged (early-bias) press loses the contested set
	f, a = mk(TieBreakLikelihood, "clean")
	f.conns["B"].AddFlag(FlagEarlyBias, 0)
	f.conns["B"].AddFlag(FlagEarlyBias, 0) // conservative
	a = f.arm(t, 700, "clean")
	f.press(a, "B", 1000)
	f.press(a, "A", 1003)
	res = resolveNow(t, a)
	require.Equal(t, "cleanFirst", res.Rule)
	require.Equal(t, "A", res.WinnerID)
	// resolution mode: 3 ms apart is not a tie
	s := base
	s.TieMode = TieResolution
	g := newFixture(t, s, [][3]float64{{10, 0.5}, {12, 0.5}}, "A", "B")
	g.now = 400
	b := g.arm(t, 700, "res")
	g.press(b, "B", 1000)
	g.press(b, "A", 1003)
	res = resolveNow(t, b)
	require.False(t, res.Contested)
	require.Equal(t, "B", res.WinnerID)
}

func TestModes(t *testing.T) {
	t.Run("serverArrival", func(t *testing.T) {
		s := DefaultSettings()
		s.Mode = ModeServerArrival
		f := newFixture(t, s, [][3]float64{{10, 0.5}, {80, 5}}, "A", "B")
		f.now = 400
		a := f.arm(t, 700, "arrival")
		for _, p := range a.Plans() {
			require.Equal(t, "onReceipt", p.Msg.Mode)
			require.Equal(t, 700.0, p.SendAt)
		}
		require.Equal(t, StatusFalseStart, a.OnPress(PressIn{PlayerID: "B"}, 699).Status)
		require.Equal(t, StatusPending, a.OnPress(PressIn{PlayerID: "A"}, 1005).Status)
		dl, _ := a.NextDeadline()
		require.Equal(t, 1005.0, dl, "no window: resolve at once")
		res := resolveNow(t, a)
		require.Equal(t, "A", res.WinnerID)
		require.Equal(t, SourceArrival, res.Ranking[0].Source)
		require.Equal(t, "first", res.Rule)
	})
	t.Run("randomWindow", func(t *testing.T) {
		s := DefaultSettings()
		s.Mode = ModeRandomWindow
		f := newFixture(t, s, [][3]float64{{10, 0.5}, {80, 5}, {250, 15}}, "A", "B", "C")
		f.now = 400
		a := f.arm(t, 700, "rw")
		require.Equal(t, StatusPending, a.OnPress(PressIn{PlayerID: "A"}, 1005).Status)
		dl, _ := a.NextDeadline()
		require.Equal(t, 1005+float64(s.RandomWindowMs), dl)
		require.Equal(t, StatusPending, a.OnPress(PressIn{PlayerID: "B"}, 1200).Status)
		res, done := a.Resolve(dl)
		require.True(t, done)
		require.True(t, res.Contested)
		require.Contains(t, []string{"A", "B"}, res.WinnerID)
		require.Equal(t, TieBreakRandom, res.TieBreak)
		require.Equal(t, StatusLate, a.OnPress(PressIn{PlayerID: "C"}, dl+1).Status)
		// the pick is reproducible from the seed and uniform over seeds
		wins := map[string]int{}
		for i := 0; i < 60; i++ {
			g := newFixture(t, s, [][3]float64{{10, 0.5}, {80, 5}}, "A", "B") // fresh conns: the rate limiter is per connection
			g.now = 400
			b := g.arm(t, 700, "rw"+string(rune('a'+i)))
			b.OnPress(PressIn{PlayerID: "A"}, 1005)
			b.OnPress(PressIn{PlayerID: "B"}, 1200)
			r, _ := b.Resolve(1400)
			wins[r.WinnerID]++
		}
		require.Greater(t, wins["A"], 15)
		require.Greater(t, wins["B"], 15)
	})
	t.Run("clientReaction", func(t *testing.T) {
		s := DefaultSettings()
		s.Mode = ModeClientReaction
		f := newFixture(t, s, [][3]float64{{10, 0.5}, {80, 5}, {250, 15}}, "A", "B", "C")
		f.now = 400
		a := f.arm(t, 700, "cr")
		// A arrives first but reports a slower reaction; B is faster by its own clock.
		require.Equal(t, StatusPending, a.OnPress(PressIn{PlayerID: "A", LitLocal: 100, PressLocal: 350}, 1005).Status)
		dl, _ := a.NextDeadline()
		require.Equal(t, 1005+float64(s.AcceptWindowMs), dl)
		require.Equal(t, StatusPending, a.OnPress(PressIn{PlayerID: "B", LitLocal: 100, PressLocal: 300}, 1030).Status)
		require.Equal(t, StatusPending, a.OnPress(PressIn{PlayerID: "C", LitLocal: 0, PressLocal: 300}, 1110).Status) // missing delta = worst
		dl, _ = a.NextDeadline()
		require.Equal(t, 1110.0, dl, "everyone pressed: close now")
		res, done := a.Resolve(dl)
		require.True(t, done)
		require.Equal(t, "B", res.WinnerID)
		require.Equal(t, 200.0, res.Ranking[0].ReactionMs)
		require.Equal(t, float64(s.AcceptWindowMs), res.Ranking[2].ReactionMs)
	})
	t.Run("writtenAll", func(t *testing.T) {
		s, _ := Preset(PresetNoRace)
		f := newFixture(t, s, [][3]float64{{10, 0.5}, {80, 5}}, "A", "B")
		a := NewArm(f.now, f.now+100, f.players, s, []byte("w"))
		require.Equal(t, StateResolved, a.State())
		require.Empty(t, a.Plans())
		res, done := a.Resolve(f.now)
		require.True(t, done)
		require.Equal(t, KindAllPlay, res.Kind)
		require.ElementsMatch(t, []string{"A", "B"}, res.Tie)
		_, ok := a.NextDeadline()
		require.False(t, ok)
	})
}

func TestLockoutGrowth(t *testing.T) {
	s := DefaultSettings() // 3000 ×2 within 10 s, cap 6000
	f := newFixture(t, s, [][3]float64{{10, 0.5}}, "A")
	f.now = 400
	a := f.arm(t, 700, "lock")
	require.Equal(t, 1000+3000.0, a.OnMisfire("A", 1000))
	require.Equal(t, 5000+6000.0, a.OnMisfire("A", 5000))
	require.Equal(t, 9000+6000.0, a.OnMisfire("A", 9000), "capped at 6000")
	require.Equal(t, 30000+3000.0, a.OnMisfire("A", 30000), "resets after 10 s")
	require.Equal(t, 33000.0, f.conns["A"].LockedUntil())
	// lockout persists across arms
	b := f.arm(t, 31000, "lock2")
	require.Equal(t, StatusLockedOut, f.press(b, "A", 31300).Status)
}

func TestUnsyncedPlayerIsArmedOnReceiptAndServerScored(t *testing.T) {
	s := DefaultSettings()
	f := newFixture(t, s, [][3]float64{{10, 0.5}}, "A")
	cold := NewConn(0)
	f.players = append(f.players, Player{ID: "U", Conn: cold, Eligible: true})
	f.offsets["U"], f.rtts["U"] = 0, 0
	f.now = 400
	a := f.arm(t, 700, "cold")
	var plan ArmPlan
	for _, p := range a.Plans() {
		if p.PlayerID == "U" {
			plan = p
		}
	}
	require.Equal(t, "onReceipt", plan.Msg.Mode)
	require.Equal(t, 700.0, plan.SendAt)
	require.Equal(t, StatusPending, a.OnPress(PressIn{PlayerID: "U", PressLocal: 950, LitLocal: 700}, 950).Status)
	res := resolveNow(t, a)
	require.Equal(t, SourceServer, res.Ranking[0].Source)
	require.InDelta(t, 250, res.Ranking[0].ReactionMs, 1)
}

func TestPresetsAndValidation(t *testing.T) {
	for _, n := range PresetNames {
		s, ok := Preset(n)
		require.True(t, ok, n)
		require.NoError(t, s.Validate(), n)
	}
	_, ok := Preset("nope")
	require.False(t, ok)
	require.Equal(t, NetWiFi, DefaultSettings().NetProfile)
	s := DefaultSettings()
	s.MinHumanMs = 50
	require.ErrorIs(t, s.Validate(), ErrInvalidSettings)
	s = DefaultSettings()
	s.ArmJitterMaxMs = 100
	require.Error(t, s.Validate())
	s = DefaultSettings()
	s.TieBreak = "coin"
	require.Error(t, s.Validate())
	s = DefaultSettings()
	s.Mode = ModeRandomWindow
	s.RandomWindowMs = 10
	require.Error(t, s.Validate())
}
