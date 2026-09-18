package buzzer

import (
	"container/heap"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Deterministic network + client simulator (in-package test helper).
// ---------------------------------------------------------------------------

type simNet struct {
	name         string
	rttLo, rttHi float64 // base RTT per connection
	sigLo, sigHi float64 // RTT jitter SD per connection
	spikeP       float64 // probability of a bufferbloat spike on a packet
	spikeMax     float64 // spike size U(0, spikeMax)
	stallP       float64 // probability of a 200 ms TCP RTO stall on a packet
	stallOnce    bool    // exactly one stall: on the first PRESS of the first question
}

var (
	netLAN   = simNet{name: "lan", rttLo: 5, rttHi: 15, sigLo: 0.7, sigHi: 1.4}
	netWiFi  = simNet{name: "wifi", rttLo: 40, rttHi: 80, sigLo: 5, sigHi: 15, spikeP: 0.15, spikeMax: 60}
	netWAN   = simNet{name: "wan", rttLo: 200, rttHi: 300, sigLo: 10, sigHi: 20}
	netStall = simNet{name: "wifi+rto", rttLo: 40, rttHi: 80, sigLo: 5, sigHi: 15, spikeP: 0.15, spikeMax: 60, stallP: 0.002}
)

type behaviour int

const (
	bHonest    behaviour = iota
	bShave               // claims the press 30 ms earlier than it happened
	bSyncDelay           // delays every second SYNC c4 by 200 ms (σ inflation)
	bPongDelay           // non-browser client: answers protocol pings 50 ms late
	bOnReceipt           // presses the instant BUTTON_ARM arrives
	bBot                 // presses at light + U(110, 190)
	bC4Inflate           // reports every c4 60 ms late (RTT inflation)
)

type simLink struct {
	base, sigma float64
	net         simNet
}

type simClient struct {
	id     string
	conn   *Conn
	link   simLink
	offset float64 // server − client
	beh    behaviour
	rmean  float64
	kernel bool

	seq, prevSeq int64
	prevC4       float64
	hasPrev      bool
	sampleIdx    int
	kSrtt, kVar  float64
	kN           int

	// per question
	lightAt, pressAt, trueR float64
	lastAck                 PressAck
	lastAudit               AuditEntry
	hist                    []pressRec
}

type pressRec struct {
	ack     PressAck
	audit   AuditEntry
	pressAt float64
}

type simEvent struct {
	at  float64
	seq int
	fn  func(now float64)
}

type eventHeap []simEvent

func (h eventHeap) Len() int { return len(h) }
func (h eventHeap) Less(i, j int) bool {
	if h[i].at != h[j].at {
		return h[i].at < h[j].at
	}
	return h[i].seq < h[j].seq
}
func (h eventHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *eventHeap) Push(x any)   { *h = append(*h, x.(simEvent)) }
func (h *eventHeap) Pop() any     { old := *h; n := len(old); x := old[n-1]; *h = old[:n-1]; return x }

type armRecord struct {
	res     Result
	trueR   map[string]float64
	pressAt map[string]float64
}

type sim struct {
	t       *testing.T
	rng     *rand.Rand
	now     float64
	q       eventHeap
	seqNo   int
	s       Settings
	clients []*simClient
	arm     *Arm
	armGen  int
	done    bool
	stop    float64
	records []armRecord
	stalled bool
	qIndex  int
}

func newSim(t *testing.T, seed int64, s Settings) *sim {
	return &sim{t: t, rng: rand.New(rand.NewSource(seed)), s: s, stop: math.Inf(1)}
}

func (s *sim) at(t float64, fn func(float64)) {
	if t < s.now {
		t = s.now
	}
	s.seqNo++
	heap.Push(&s.q, simEvent{at: t, seq: s.seqNo, fn: fn})
}

func (s *sim) runUntil(t float64) {
	for s.q.Len() > 0 && s.q[0].at <= t {
		e := heap.Pop(&s.q).(simEvent)
		s.now = e.at
		e.fn(e.at)
	}
	if t > s.now {
		s.now = t
	}
}

func (s *sim) addClient(id string, net simNet, beh behaviour, rmean float64, kernel bool) *simClient {
	base := net.rttLo + s.rng.Float64()*(net.rttHi-net.rttLo)
	sig := net.sigLo + s.rng.Float64()*(net.sigHi-net.sigLo)
	c := &simClient{id: id, conn: NewConn(s.now), link: simLink{base: base, sigma: sig, net: net}, beh: beh, rmean: rmean, kernel: kernel,
		offset: (s.rng.Float64() - 0.5) * 2e6}
	s.clients = append(s.clients, c)
	return c
}

// owd draws a one-way delay; rtt returns the matching round trip for a ping.
func (s *sim) owd(c *simClient, allowStall bool) float64 {
	l := c.link
	// queueing delay is one-sided: half-normal with scale σ/√2 per direction
	d := l.base/2 + math.Abs(s.rng.NormFloat64())*l.sigma/math.Sqrt2
	if d < 0.3 {
		d = 0.3
	}
	if l.net.spikeP > 0 && s.rng.Float64() < l.net.spikeP {
		d += s.rng.Float64() * l.net.spikeMax
	}
	if allowStall && l.net.stallP > 0 && s.rng.Float64() < l.net.stallP {
		d += 200
	}
	return d
}

func (s *sim) sync(c *simClient, t float64) {
	c1 := t - c.offset
	in := SyncIn{Seq: c.seq, C1: c1, PrevSeq: c.prevSeq, PrevC4: c.prevC4, HasPrev: c.hasPrev}
	c.seq++
	up := s.owd(c, true)
	s.at(t+up, func(now float64) {
		out := c.conn.OnSync(in, now, now+0.05)
		down := s.owd(c, true)
		s.at(now+0.05+down, func(t4 float64) {
			c4 := t4 - c.offset
			c.sampleIdx++
			switch c.beh {
			case bSyncDelay:
				if c.sampleIdx%2 == 0 {
					c4 += 200
				}
			case bC4Inflate:
				c4 += 60
			}
			c.prevSeq, c.prevC4, c.hasPrev = out.Seq, c4, true
		})
	})
}

func (s *sim) ping(c *simClient, t float64) {
	rtt := s.owd(c, true) + s.owd(c, true)
	// kernel sees the true transport round trip
	if c.kN == 0 {
		c.kSrtt, c.kVar = rtt, rtt/2
	} else {
		err := rtt - c.kSrtt
		c.kVar = 0.75*c.kVar + 0.25*math.Abs(err)
		c.kSrtt += err / 8
	}
	c.kN++
	if c.beh == bPongDelay {
		rtt += 50
	}
	s.at(t+rtt, func(now float64) {
		c.conn.OnWSPong(rtt, now)
		if c.kernel {
			c.conn.OnKernelRTT(c.kSrtt, c.kVar, now)
		}
	})
}

// schedule starts the background SYNC/ping schedule for every client.
func (s *sim) schedule() {
	for _, c := range s.clients {
		c := c
		for i := 0; i < SyncBurstCount; i++ {
			at := s.now + float64(i)*SyncBurstIntervalMs
			s.at(at, func(t float64) { s.sync(c, t) })
		}
		var steady func(float64)
		steady = func(t float64) {
			if t > s.stop {
				return
			}
			s.sync(c, t)
			s.at(t+SyncSteadyIntervalMs, steady)
		}
		s.at(s.now+SyncBurstCount*SyncBurstIntervalMs+SyncSteadyIntervalMs, steady)
		var pinger func(float64)
		pinger = func(t float64) {
			if t > s.stop {
				return
			}
			s.ping(c, t)
			iv := WSPingIdleMs
			if s.arm != nil && !s.done {
				iv = WSPingArmedMs
			}
			s.at(t+iv, pinger)
		}
		s.at(s.now, pinger)
	}
}

func (s *sim) client(id string) *simClient {
	for _, c := range s.clients {
		if c.id == id {
			return c
		}
	}
	return nil
}

func (s *sim) reaction(c *simClient) float64 {
	switch c.beh {
	case bOnReceipt:
		return 0
	case bBot:
		return 110 + s.rng.Float64()*80
	}
	r := c.rmean + s.rng.NormFloat64()*40
	if r < 150 {
		r = 150
	}
	return r
}

// question plays one buzzer question and returns its record.
func (s *sim) question() armRecord {
	s.qIndex++
	readEnd := s.now + 2000
	for _, c := range s.clients {
		c := c
		for i := 0; i < PreArmBurstCount; i++ {
			s.at(readEnd-600+float64(i)*PreArmBurstIntervalMs, func(t float64) { s.sync(c, t) })
		}
	}
	rec := armRecord{trueR: map[string]float64{}, pressAt: map[string]float64{}}
	s.done = false
	s.at(readEnd, func(now float64) {
		var players []Player
		for _, c := range s.clients {
			players = append(players, Player{ID: c.id, Conn: c.conn, Eligible: true})
		}
		s.armGen++
		gen := s.armGen
		arm := NewArm(now, readEnd, players, s.s, []byte(fmt.Sprintf("q%d", s.qIndex)))
		s.arm = arm
		for _, pl := range arm.Plans() {
			c := s.client(pl.PlayerID)
			msg := pl.Msg
			s.at(pl.SendAt, func(t float64) {
				arm.OnSent(c.id, t)
				down := s.owd(c, true)
				s.at(t+down, func(tr float64) { s.onArm(arm, gen, c, msg, tr, &rec) })
			})
		}
		s.deadline(arm, gen)
	})
	for !s.done && s.q.Len() > 0 {
		e := heap.Pop(&s.q).(simEvent)
		s.now = e.at
		e.fn(e.at)
	}
	rec.res = s.arm.ForceResolve(s.now)
	s.records = append(s.records, rec)
	s.runUntil(s.now + 300)
	return rec
}

func (s *sim) onArm(arm *Arm, gen int, c *simClient, msg ArmMsg, tArrive float64, rec *armRecord) {
	up := s.owd(c, true)
	s.at(tArrive+up, func(t float64) { arm.OnArmAck(c.id, t, tArrive-c.offset) })
	lightAt := tArrive
	if msg.Mode == "scheduled" && msg.ArmAtLocal+c.offset > tArrive {
		lightAt = msg.ArmAtLocal + c.offset
	}
	r := s.reaction(c)
	pressAt := lightAt + r
	c.lightAt, c.pressAt, c.trueR = lightAt, pressAt, r
	rec.trueR[c.id] = r
	rec.pressAt[c.id] = pressAt
	pressLocal := pressAt - c.offset
	if c.beh == bShave {
		pressLocal -= 30
	}
	litLocal := lightAt - c.offset
	s.at(pressAt, func(t float64) {
		up := s.owd(c, true)
		if c.link.net.stallOnce && !s.stalled {
			up += 200
			s.stalled = true
		}
		s.at(t+up, func(tr float64) {
			if gen != s.armGen {
				return
			}
			c.lastAck = arm.OnPress(PressIn{PlayerID: c.id, Seq: int64(s.qIndex), PressLocal: pressLocal, LitLocal: litLocal, Src: "pointer"}, tr)
			if au := arm.Audit(); len(au) > 0 {
				c.lastAudit = au[len(au)-1]
			}
			c.hist = append(c.hist, pressRec{ack: c.lastAck, audit: c.lastAudit, pressAt: c.pressAt})
			s.deadline(arm, gen)
		})
	})
}

func (s *sim) deadline(arm *Arm, gen int) {
	at, ok := arm.NextDeadline()
	if !ok {
		return
	}
	s.at(at, func(t float64) {
		if gen != s.armGen || s.done {
			return
		}
		if _, done := arm.Resolve(t); done {
			s.done = true
			return
		}
		s.deadline(arm, gen)
	})
}

// ---------------------------------------------------------------------------
// Statistics
// ---------------------------------------------------------------------------

type winStats struct {
	name                      string
	n                         int
	total, ge10, ge20, ge40   int
	ok10, ok20, ok40          int
	contested, clamped, wrong int
}

func (w *winStats) add(rec armRecord, honest map[string]bool) {
	type kv struct {
		id string
		r  float64
	}
	var xs []kv
	for id, r := range rec.trueR {
		if honest[id] {
			xs = append(xs, kv{id, r})
		}
	}
	if len(xs) < 2 {
		return
	}
	sort.Slice(xs, func(i, j int) bool { return xs[i].r < xs[j].r })
	gap := xs[1].r - xs[0].r
	won := rec.res.WinnerID == xs[0].id
	w.total++
	if rec.res.Contested {
		w.contested++
	}
	for _, r := range rec.res.Ranking {
		if r.Source == SourceClamped {
			w.clamped++
		}
	}
	if !won {
		w.wrong++
	}
	if gap >= 10 {
		w.ge10++
		if won {
			w.ok10++
		}
	}
	if gap >= 20 {
		w.ge20++
		if won {
			w.ok20++
		}
	}
	if gap >= 40 {
		w.ge40++
		if won {
			w.ok40++
		}
	}
}

func pct(a, b int) float64 {
	if b == 0 {
		return 100
	}
	return 100 * float64(a) / float64(b)
}

func (w *winStats) String() string {
	return fmt.Sprintf("%-12s arms=%4d  gap>=10: %5.1f%% (n=%d)  gap>=20: %5.1f%% (n=%d)  gap>=40: %5.1f%% (n=%d)  contested=%d clamped=%d",
		w.name, w.total, pct(w.ok10, w.ge10), w.ge10, pct(w.ok20, w.ge20), w.ge20, pct(w.ok40, w.ge40), w.ge40, w.contested, w.clamped)
}

func presetFor(net simNet) Settings {
	switch net.name {
	case "lan":
		s, _ := Preset(PresetLANWired)
		return s
	case "wan":
		s, _ := Preset(PresetInternetFair)
		return s
	}
	return DefaultSettings()
}

// runHonest plays n questions with honest clients on the given nets.
func runHonest(t *testing.T, name string, seed int64, s Settings, nets []simNet, n int) *winStats {
	sm := newSim(t, seed, s)
	honest := map[string]bool{}
	for i, net := range nets {
		id := fmt.Sprintf("P%d", i+1)
		sm.addClient(id, net, bHonest, 250, false)
		honest[id] = true
	}
	sm.schedule()
	sm.runUntil(20000) // warm-up: bursts + 16 s of pongs
	w := &winStats{name: name}
	for i := 0; i < n; i++ {
		w.add(sm.question(), honest)
	}
	return w
}

func TestSimulatorHonestFairness(t *testing.T) {
	if testing.Short() {
		t.Skip("simulator")
	}
	const n = 400
	rows := []*winStats{
		runHonest(t, "lan", 1, presetFor(netLAN), []simNet{netLAN, netLAN, netLAN}, n),
		runHonest(t, "wifi", 2, presetFor(netWiFi), []simNet{netWiFi, netWiFi, netWiFi}, n),
		runHonest(t, "wan", 3, presetFor(netWAN), []simNet{netWAN, netWAN, netWAN}, n),
		runHonest(t, "mixed", 4, presetFor(netWAN), []simNet{netLAN, netWiFi, netWAN}, n),
		runHonest(t, "wifi+rto", 5, presetFor(netWiFi), []simNet{netStall, netStall, netStall}, n),
	}
	// the same mixed room on the default preset: the WAN guest is above maxRttMs, so it is
	// armed on receipt and server-scored; its races become calibrated lotteries (no assertion)
	fallback := runHonest(t, "mixed/wifiParty", 4, DefaultSettings(), []simNet{netLAN, netWiFi, netWAN}, n)
	for _, w := range append(rows, fallback) {
		t.Log(w.String())
	}
	for _, w := range rows {
		require.GreaterOrEqual(t, pct(w.ok20, w.ge20), 99.0, "%s: faster reaction must win at gap >= 20 ms", w.name)
		require.GreaterOrEqual(t, pct(w.ok40, w.ge40), 99.5, "%s: faster reaction must win at gap >= 40 ms", w.name)
	}
	require.GreaterOrEqual(t, pct(rows[1].ok10, rows[1].ge10), 95.0, "wifi: faster reaction must win at gap >= 10 ms")
	require.GreaterOrEqual(t, pct(rows[0].ok10, rows[0].ge10), 99.0, "lan: faster reaction must win at gap >= 10 ms")
	// SIGame's FirstWins on the same mixed room: the LAN player wins almost everything
	arrival := DefaultSettings()
	arrival.Mode = ModeServerArrival
	w := runHonest(t, "mixed/arrival", 4, arrival, []simNet{netLAN, netWiFi, netWAN}, 200)
	t.Log(w.String())
	require.Less(t, pct(w.ok20, w.ge20), 90.0, "serverArrival is ping-biased by construction")
}

// TestSimulatorSingleStall: one 200 ms TCP RTO stall on the fastest player's PRESS
// costs (stall − tol) and is flagged out_of_bounds; a single clamp does not touch trust.
func TestSimulatorSingleStall(t *testing.T) {
	s := DefaultSettings()
	sm := newSim(t, 77, s)
	stallNet := netWiFi
	stallNet.stallOnce = true
	stallNet.spikeP = 0
	a := sm.addClient("A", stallNet, bHonest, 160, false) // the fastest; its first press stalls
	sm.addClient("B", netWiFi, bHonest, 420, false)
	sm.schedule()
	sm.runUntil(20000)
	rec := sm.question()
	require.True(t, sm.stalled)
	require.Equal(t, StatusPending, a.lastAck.Status)
	require.Equal(t, SourceClamped, a.lastAudit.Source)
	require.Contains(t, a.lastAudit.Flags, FlagOutOfBounds)
	require.Equal(t, 0, a.conn.Trust().Score, "a single clamp is not charged")
	lost := a.lastAudit.TFinal - a.pressAt
	require.InDelta(t, 200-a.lastAudit.Model.TolMs, lost, 12, "loss = stall − tol")
	require.Equal(t, "A", rec.res.WinnerID, "A was ~260 ms faster and still wins after the clamp")
}

// ---------------------------------------------------------------------------
// Adversarial clients
// ---------------------------------------------------------------------------

func runAdversary(t *testing.T, seed int64, net simNet, beh behaviour, kernel bool, n int) (*sim, *simClient, []*simClient) {
	s := presetFor(net)
	sm := newSim(t, seed, s)
	cheat := sm.addClient("X", net, beh, 220, kernel)
	h1 := sm.addClient("H1", net, bHonest, 250, kernel)
	h2 := sm.addClient("H2", net, bHonest, 250, kernel)
	sm.schedule()
	sm.runUntil(25000)
	for i := 0; i < n; i++ {
		sm.question()
	}
	return sm, cheat, []*simClient{h1, h2}
}

// gain is how much earlier than the physical press the server scored a press (ms).
func gain(c *simClient) float64 {
	if c.lastAck.Status != StatusPending {
		return 0
	}
	return c.pressAt - c.lastAudit.TFinal
}

func TestAdversaryStampShaving(t *testing.T) {
	for _, net := range []simNet{netLAN, netWiFi} {
		t.Run(net.name, func(t *testing.T) {
			// 8 questions: the cheater is still scored by its stamps, so the per-press bound is visible
			sm, x, _ := runAdversary(t, 21, net, bShave, false, 8)
			accepted, clamped, maxGain, tol := 0, 0, 0.0, 0.0
			for _, h := range x.hist {
				if h.ack.Status != StatusPending {
					continue
				}
				accepted++
				tol = h.audit.Model.TolMs
				g := h.pressAt - h.audit.TFinal
				// the bound is relative to the server's own estimate; versus the physical
				// press it is loosened only by the one-way jitter of that packet
				require.LessOrEqual(t, h.audit.TArr-h.audit.TFinal, h.audit.Model.TolMs+1e-6, "gain vs tArr is bounded by tol")
				require.LessOrEqual(t, g, h.audit.Model.TolMs+h.audit.Model.RttRefMs/2, "gain vs physical press")
				if g > maxGain {
					maxGain = g
				}
				if h.audit.Source == SourceClamped {
					clamped++
				}
			}
			require.GreaterOrEqual(t, accepted, 4)
			fl := x.conn.Trust().Flags
			if net.name == "lan" {
				require.Equal(t, accepted, clamped, "30 ms shave on LAN is always clamped (tol %.1f)", tol)
				require.Greater(t, fl[FlagOutOfBounds], 0)
				require.Greater(t, fl[FlagClampRatio], 0)
			} else {
				require.Greater(t, maxGain, 20.0, "30 ms shave on WiFi sits inside tol")
			}
			t.Logf("%s after 8: tol=%.1f accepted=%d clamped=%d maxGain=%.1f flags=%v trust=%+v", net.name, tol, accepted, clamped, maxGain, fl, x.conn.Trust())
			// 20 questions: the sustained bias is flagged and the ladder demotes the cheater
			sm, x, _ = runAdversary(t, 21, net, bShave, false, 32)
			fl = x.conn.Trust().Flags
			var bias []float64
			for i := 0; i < x.conn.bias.n; i++ {
				bias = append(bias, x.conn.bias.newest(i))
			}
			var sts []string
			for _, h := range x.hist {
				sts = append(sts, h.ack.Status+"/"+h.audit.Source)
			}
			t.Logf("%s after 32: hist=%v bias=%v σ=%.1f resid=%.1f", net.name, sts, bias, x.lastAudit.Model.SigmaMs, x.lastAudit.Model.TolMs-3*x.lastAudit.Model.SigmaMs-2)
			require.Greater(t, fl[FlagEarlyBias], 0, "flags=%v", fl)
			require.NotEqual(t, TrustFull, x.conn.Trust().Level)
			wins := 0
			for _, rec := range sm.records[10:] {
				if rec.res.WinnerID == "X" {
					wins++
				}
			}
			t.Logf("%s after 32: flags=%v trust=%+v wins(last 10)=%d", net.name, fl, x.conn.Trust(), wins)
		})
	}
}

// TestAdversarySyncDelay is the regression test for judge 3's hole: delaying
// every second SYNC reply must not widen σ/tol because the ws anchor caps them.
func TestAdversarySyncDelay(t *testing.T) {
	_, x, hs := runAdversary(t, 22, netWiFi, bSyncDelay, false, 12)
	mx, mh := x.conn.Model(x.lastAudit.TRecv), hs[0].conn.Model(x.lastAudit.TRecv)
	t.Logf("cheater σ=%.1f tol=%.1f rttRef=%.1f rttWs=%.1f | honest σ=%.1f tol=%.1f rttRef=%.1f flags=%v", mx.SigmaMs, mx.TolMs, mx.RttRefMs, mx.RttWsMs, mh.SigmaMs, mh.TolMs, mh.RttRefMs, x.conn.Trust().Flags)
	require.LessOrEqual(t, mx.SigmaMs, SigmaMaxMs)
	require.LessOrEqual(t, mx.TolMs, mh.TolMs+12, "tol may not grow beyond the anchor slack")
	require.LessOrEqual(t, mx.RttRefMs, mx.RttWsMs+AnchorSlackWSMs+1)
	require.True(t, hasFlag(x.conn, FlagJitterInflated))
	require.LessOrEqual(t, gain(x), mx.TolMs+1.5)
}

func TestAdversaryPongDelay(t *testing.T) {
	// A non-browser client that delays pongs by 50 ms: with the kernel anchor the
	// transport lie is flagged rtt_inflated; without it the app samples fall
	// below the (inflated) floor and are flagged sync_lie. Either way the gain is bounded.
	_, x, _ := runAdversary(t, 23, netLAN, bPongDelay, true, 12)
	require.True(t, hasFlag(x.conn, FlagRttInflated), "flags=%v", x.conn.Trust().Flags)
	m := x.conn.Model(x.lastAudit.TRecv)
	require.LessOrEqual(t, m.RttRefMs, m.RttKernelMs+AnchorSlackKernelMs+0.1)
	require.LessOrEqual(t, math.Abs(gain(x)), m.TolMs+AnchorSlackKernelMs, "gain=%.1f", gain(x))
	_, y, _ := runAdversary(t, 24, netLAN, bPongDelay, false, 12)
	require.True(t, hasFlag(y.conn, FlagSyncLie), "flags=%v", y.conn.Trust().Flags)
	require.Less(t, y.conn.Trust().Score, -1)
	if y.lastAck.Status == StatusPending {
		require.LessOrEqual(t, math.Abs(gain(y)), y.lastAudit.Model.TolMs+AnchorSlackWSMs)
	}
	t.Logf("kernel: gain=%.1f trust=%+v | ws-only: status=%s gain=%.1f trust=%+v", gain(x), x.conn.Trust(), y.lastAck.Status, gain(y), y.conn.Trust())
}

func TestAdversaryLightOnReceipt(t *testing.T) {
	sm, x, _ := runAdversary(t, 25, netWiFi, bOnReceipt, false, 6)
	wins := 0
	for _, rec := range sm.records {
		if rec.res.WinnerID == "X" {
			wins++
		}
	}
	require.Equal(t, 0, wins)
	require.True(t, hasFlag(x.conn, FlagTooEarly), "flags=%v", x.conn.Trust().Flags)
	require.Contains(t, []string{StatusTooEarly, StatusFalseStart, StatusLockedOut}, x.lastAck.Status)
	require.Greater(t, x.conn.LockedUntil(), 0.0)
}

func TestAdversaryHumanizedBot(t *testing.T) {
	sm, x, _ := runAdversary(t, 26, netWiFi, bBot, false, 12)
	wins := 0
	for _, rec := range sm.records {
		if rec.res.WinnerID == "X" {
			wins++
		}
	}
	require.GreaterOrEqual(t, wins, 9, "a humanized bot beats humans (nothing in the netcode can stop it)")
	require.True(t, hasFlag(x.conn, FlagBotLike), "flags=%v", x.conn.Trust().Flags)
	// the badge appears only once enough presses exist
	y := NewConn(0)
	for i := 0; i < BotMinPresses-1; i++ {
		y.noteReaction(150, true, false, float64(i))
	}
	require.False(t, hasFlag(y, FlagBotLike))
}

func TestAdversaryRttInflation(t *testing.T) {
	_, x, hs := runAdversary(t, 27, netWiFi, bC4Inflate, false, 12)
	mx, mh := x.conn.Model(x.lastAudit.TRecv), hs[0].conn.Model(x.lastAudit.TRecv)
	require.True(t, hasFlag(x.conn, FlagRttInflated), "flags=%v", x.conn.Trust().Flags)
	require.LessOrEqual(t, mx.RttRefMs, mx.RttWsMs+AnchorSlackWSMs+1)
	require.LessOrEqual(t, gain(x), mx.TolMs+AnchorSlackWSMs/2+1.5)
	t.Logf("cheater rttRef=%.1f (ws %.1f) tol=%.1f gain=%.1f | honest rttRef=%.1f", mx.RttRefMs, mx.RttWsMs, mx.TolMs, gain(x), mh.RttRefMs)
}
