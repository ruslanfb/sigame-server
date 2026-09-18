package buzzer

import (
	"math"
	"sort"
)

// Measurement constants (values from the syntheses; see docs/buzzer.md).
const (
	SyncBurstCount        = 10     // SYNC samples in a burst on connect/reconnect/visible
	SyncBurstIntervalMs   = 100.0  // interval inside a burst
	SyncSteadyIntervalMs  = 1000.0 // steady-state SYNC interval
	PreArmBurstCount      = 5      // SYNC samples when question reading starts
	PreArmBurstIntervalMs = 100.0  // interval of the pre-arm burst
	WSPingIdleMs          = 1000.0 // WebSocket protocol ping interval while idle
	WSPingArmedMs         = 250.0  // WebSocket protocol ping interval while reading/armed
	ConnQualityIntervalMs = 5000.0 // CONN_QUALITY broadcast interval

	AppRingSize    = 64      // app SYNC samples kept
	PendingSyncs   = 8       // SYNC 4-tuples kept while their c4 is in flight
	WSRingSize     = 32      // WebSocket pong samples kept
	MADWindow      = 16      // samples for the MAD jitter estimate
	RttMinWindowMs = 30000.0 // sliding window for rtt_min and for offset candidates
	EwmaAlpha      = 1.0 / 8 // RFC 6298 srtt gain
	RttVarBeta     = 1.0 / 4 // RFC 6298 rttvar gain
	MADScale       = 1.4826  // MAD → σ for a Gaussian
	MaxSampleRttMs = 10000.0 // samples above this are discarded

	AnchorSlackWSMs     = 15.0 // rttRef ceiling above the ws pong EWMA
	AnchorSlackKernelMs = 10.0 // rttRef ceiling above the kernel srtt
	AnchorFloorMarginMs = 1.0  // app samples below ws_min − margin − σ_ws are lies
	WSFloorMinSamples   = 8    // pongs needed before the ws floor is enforced
	SigmaSlackMs        = 3.0  // σ ceiling above the anchor jitter
	SigmaMinMs          = 0.5
	SigmaMaxMs          = 30.0
	SigmaColdMs         = 20.0 // σ assumed before any app sample exists
	ResidFrac           = 0.10 // residual (offset) margin as a fraction of rtt_min
	ResidMinMs          = 2.0
	ResidMaxMs          = 10.0
	EpsInputMs          = 2.0 // timer/event dispatch resolution; per-press error of a client stamp
	TolMinMs            = 5.0
	DefaultTolCapMs     = 60.0
	LeadBaseMs          = 20.0
	LeadMinMs           = 25.0
	LeadMaxMs           = 400.0
	StepMarginMs        = 5.0 // step detector threshold is u + this
	StepRun             = 3   // consecutive deviating samples that trigger a reset

	UnsyncedWindowMs   = 10000.0 // need >= UnsyncedMinSamples in this window
	UnsyncedMinSamples = 5
	QualityGoodRttMs   = 30.0
	QualityGoodSigmaMs = 5.0
	QualityFairSigmaMs = 20.0
	InflationWindowMs  = 6000.0 // 3 consecutive 2 s windows
	JitterInflFactor   = 2.0    // jitter_inflated if σ_app > factor·σ_anchor + margin
	JitterInflMarginMs = 5.0
	SyncLieMinGapMs    = 2000.0 // sync_lie is charged at most once per this interval

	WaitPerPlayerMaxMs    = 200.0 // cap on how long one pending player can hold the window
	EarlyBiasWindow       = 16    // presses whose median feeds the early-bias detector
	EarlyBiasRepeat       = 8     // re-charge every N presses while the bias persists
	ClampRatioRepeat      = 10    // re-charge every N presses while the clamp ratio persists
	BotRepeat             = 8     // re-charge every N presses while the bot signature persists
	BotMinPresses         = 8
	BotMedianMs           = 170.0
	BotMADMs              = 25.0
	ClampRatioWindow      = 10 // presses in the clamp-ratio window
	ClampRatioMinPresses  = 5
	ClampRatio            = 0.30
	OutOfBoundsMinMs      = 5.0 // clamps smaller than this are not flagged
	AckLateWindow         = 5   // arms in the ack_late window
	AckLateCount          = 4   // ack_late if >= this many late acks in the window
	RateLimitPresses      = 5   // more than this many PRESS per second is rate_limit
	RateLimitWindowMs     = 1000.0
	LockoutRepeatWindowMs = 10000.0 // lockout grows for repeats inside this window
	ReactionHistory       = 16      // reactions kept for the bot detector

	TrustCleanStreak            = 5 // clean presses per +1
	ThresholdConservative       = -2
	ThresholdServerOnly         = -4
	ThresholdArrivalOnly        = -6
	ThresholdStrictConservative = -1
	ThresholdStrictServerOnly   = -2
	ThresholdStrictArrivalOnly  = -3
)

// Flag names (see docs/buzzer.md for meaning and trust impact).
const (
	FlagSyncLie        = "sync_lie"        // app RTT below the transport floor (client shortened c4−c1)
	FlagRttInflated    = "rtt_inflated"    // app RTT (or ws RTT vs kernel) above the anchor for 6 s
	FlagJitterInflated = "jitter_inflated" // app jitter far above the transport jitter for 6 s
	FlagOutOfBounds    = "out_of_bounds"   // client stamp clamped by more than 5 ms (count only)
	FlagClampRatio     = "clamp_ratio"     // > 30 % of the last 10 presses clamped (scoring)
	FlagTooEarly       = "too_early"       // reaction below the human floor: rejected + lockout
	FlagEarlyBias      = "early_bias"      // mean(tClient − tArr) over 10 presses below −(σ + resid + 2)
	FlagBotLike        = "bot_like"        // >= 8 presses, median < 170 ms and MAD < 25 ms
	FlagAckLate        = "ack_late"        // ARM_ACK later than allowed in >= 3 of 5 arms
	FlagRateLimit      = "rate_limit"      // > 5 PRESS per second
	FlagStale          = "stale"           // press for an inactive arm (count only)
	FlagDuplicate      = "duplicate"       // second press in one arm (count only)
	FlagLateBeat       = "late_beat"       // audit only: a late press that would have won
)

// scoringFlags cost one trust point each.
var scoringFlags = map[string]bool{
	FlagSyncLie: true, FlagRttInflated: true, FlagJitterInflated: true, FlagClampRatio: true,
	FlagTooEarly: true, FlagEarlyBias: true, FlagBotLike: true, FlagAckLate: true, FlagRateLimit: true,
}

// Quality is the sync badge shown to players.
type Quality string

// Quality levels.
const (
	QualityUnsynced Quality = "unsynced"
	QualityGood     Quality = "good"
	QualityFair     Quality = "fair"
	QualityPoor     Quality = "poor"
)

// Model is the public clock/RTT model of a connection, sent to clients in
// SYNC_ACK and CONN_QUALITY.
type Model struct {
	OffsetMs    float64  `json:"offsetMs" doc:"Server minus client clock (ms): serverTime = clientLocal + offsetMs."`
	RttRefMs    float64  `json:"rttRefMs" doc:"Anchored reference round-trip time used for rewind and tolerances."`
	RttWsMs     float64  `json:"rttWsMs" doc:"WebSocket ping/pong RTT EWMA (tamper-proof anchor), 0 if none."`
	RttKernelMs float64  `json:"rttKernelMs" doc:"Kernel TCP smoothed RTT, 0 if unavailable."`
	SigmaMs     float64  `json:"sigmaMs" doc:"Jitter estimate (ms), capped by the transport anchor and 30 ms."`
	TolMs       float64  `json:"tolMs" doc:"Trust radius for client stamps (ms)."`
	UMs         float64  `json:"uMs" doc:"Displayed sync uncertainty ±u (ms)."`
	LeadMs      float64  `json:"leadMs" doc:"How early BUTTON_ARM is sent before the light (ms)."`
	Quality     Quality  `json:"quality" doc:"unsynced | good | fair | poor."`
	Flags       []string `json:"flags,omitempty" doc:"Integrity flags currently on record for this connection."`
	Samples     int      `json:"samples" doc:"Accepted SYNC samples in the ring."`
}

// snapshot is the internal model with the extra quantities the arm needs.
type snapshot struct {
	Model
	resid       float64
	rttMin      float64
	rttMax16    float64
	rttMinRef   float64 // max(app rtt_min, anchor floor): the fastest round trip the server believes
	sigmaApp    float64
	sigmaAnchor float64 // +Inf when no anchor
	anchorLo    float64 // -Inf when no anchor
	anchorHi    float64 // +Inf when no anchor
	anchored    bool
}

// TrustLevel is the scoring mode the trust ladder assigns.
type TrustLevel string

// Trust levels.
const (
	TrustFull         TrustLevel = "full"         // client stamps trusted within tol
	TrustConservative TrustLevel = "conservative" // stamps still used, but loses contested sets to clean players
	TrustServerOnly   TrustLevel = "serverOnly"   // scored by rewound arrival (tArr)
	TrustArrivalOnly  TrustLevel = "arrivalOnly"  // scored by raw arrival (tRecv)
)

// Trust is the ladder state of a connection.
type Trust struct {
	Score int            `json:"score" doc:"Trust score: -1 per flag, +1 per 5 clean presses, never above 0."`
	Level TrustLevel     `json:"level" doc:"full | conservative | serverOnly | arrivalOnly."`
	Flags map[string]int `json:"flags,omitempty" doc:"Flag counts on record."`
}

// SyncIn is a client SYNC message: c1 for this sample and c4 of the previous one.
type SyncIn struct {
	Seq     int64
	C1      float64
	PrevSeq int64
	PrevC4  float64
	HasPrev bool
}

// SyncOut is the SYNC_ACK payload.
type SyncOut struct {
	Seq        int64
	C1, S2, S3 float64
	Model      Model
}

type sample struct{ rtt, off, at float64 }

type pendingSync struct {
	seq        int64
	c1, s2, s3 float64
	ok         bool
}

type ring[T any] struct {
	buf  []T
	n    int
	head int
}

func newRing[T any](size int) ring[T] { return ring[T]{buf: make([]T, size)} }

func (r *ring[T]) push(v T) {
	r.buf[r.head] = v
	r.head = (r.head + 1) % len(r.buf)
	if r.n < len(r.buf) {
		r.n++
	}
}

// newest returns the i-th newest element (0 = most recent).
func (r *ring[T]) newest(i int) T {
	idx := (r.head - 1 - i) % len(r.buf)
	if idx < 0 {
		idx += len(r.buf)
	}
	return r.buf[idx]
}

func (r *ring[T]) reset() { r.n, r.head = 0, 0 }

// Conn is the per-connection measurement state: clock model, anchors, flags,
// trust score and false-start lockout. It persists across arms and is
// recreated on reconnect.
type Conn struct {
	// TolCapMs caps the trust radius; the room sets it from Settings.TolCapMs.
	TolCapMs float64
	// AutoTrust applies the ladder; when false Trust().Level stays full unless forced.
	AutoTrust bool
	// StrictTrust uses tournament thresholds.
	StrictTrust bool
	// ForceLevel, when non-empty, overrides the ladder (host action).
	ForceLevel TrustLevel

	createdAt float64
	pend      ring[pendingSync] // last few SYNC 4-tuples awaiting their c4

	app    ring[sample]
	ewma   float64
	rttvar float64
	ewmaN  int

	ws       ring[sample]
	wsEwma   float64
	wsEwmaN  int
	lastPong float64

	kern struct {
		srtt, rttvar, at float64
		ok               bool
	}

	offset    float64
	offsetOK  bool
	stepDir   int
	stepRun   int
	cold      bool
	sinceCold int

	flags       map[string]int
	score       int
	cleanStreak int

	rttInflSince, jitInflSince, wsInflSince float64
	rttInflFlag, jitInflFlag, wsInflFlag    bool
	lastSyncLie, lastRateFlag               float64
	bias                                    ring[float64]
	react                                   ring[float64]
	presses                                 int
	botFlag                                 bool
	botFlagAt                               int
	clampFlagAt                             int
	biasActive                              bool
	biasFlagAt                              int
	clampHist                               ring[bool]
	clampFlag                               bool
	ackLate                                 ring[bool]
	ackLateFlag                             bool
	pressTimes                              ring[float64]
	lockedUntil                             float64
	falseStarts                             ring[float64]
	scratchA, scratchB                      []float64
}

// NewConn creates a connection model at server time now.
func NewConn(now float64) *Conn {
	c := &Conn{
		TolCapMs:     DefaultTolCapMs,
		AutoTrust:    true,
		createdAt:    now,
		pend:         newRing[pendingSync](PendingSyncs),
		app:          newRing[sample](AppRingSize),
		ws:           newRing[sample](WSRingSize),
		flags:        map[string]int{},
		bias:         newRing[float64](EarlyBiasWindow),
		react:        newRing[float64](ReactionHistory),
		clampHist:    newRing[bool](ClampRatioWindow),
		ackLate:      newRing[bool](AckLateWindow),
		pressTimes:   newRing[float64](RateLimitPresses + 3),
		falseStarts:  newRing[float64](8),
		scratchA:     make([]float64, 0, AppRingSize),
		scratchB:     make([]float64, 0, AppRingSize),
		rttInflSince: -1, jitInflSince: -1, wsInflSince: -1,
		lastSyncLie: math.Inf(-1), lastRateFlag: math.Inf(-1),
	}
	return c
}

// OnSync processes a SYNC message received at s2 and answered at s3 (both
// stamped by the caller on the server monotonic clock). The returned SyncOut
// is the SYNC_ACK payload.
func (c *Conn) OnSync(in SyncIn, s2, s3 float64) SyncOut {
	if in.HasPrev {
		for i := 0; i < c.pend.n; i++ {
			if p := c.pend.newest(i); p.ok && p.seq == in.PrevSeq {
				c.acceptSample(p, in.PrevC4, s2)
				c.pend.buf[(c.pend.head-1-i+2*len(c.pend.buf))%len(c.pend.buf)].ok = false
				break
			}
		}
	}
	c.pend.push(pendingSync{seq: in.Seq, c1: in.C1, s2: s2, s3: s3, ok: true})
	return SyncOut{Seq: in.Seq, C1: in.C1, S2: s2, S3: s3, Model: c.Model(s3)}
}

func (c *Conn) acceptSample(p pendingSync, c4, now float64) {
	rtt := (c4 - p.c1) - (p.s3 - p.s2)
	off := ((p.s2 - p.c1) + (p.s3 - c4)) / 2
	if rtt < 0 || rtt > MaxSampleRttMs {
		c.syncLie(now)
		return
	}
	if floor, ok := c.floor(now); ok && rtt < floor {
		c.syncLie(now)
		return
	}
	// Step detection against the model before this sample.
	if c.offsetOK && c.app.n >= UnsyncedMinSamples {
		lim := c.snap(now).UMs + StepMarginMs
		dev := off - c.offset
		dir := 0
		if dev > lim {
			dir = 1
		} else if dev < -lim {
			dir = -1
		}
		if dir != 0 && dir == c.stepDir {
			c.stepRun++
		} else {
			c.stepDir = dir
			c.stepRun = 0
			if dir != 0 {
				c.stepRun = 1
			}
		}
		if c.stepRun >= StepRun {
			c.resetModel()
			c.cold = true
			c.sinceCold = 0
		}
	}
	c.app.push(sample{rtt: rtt, off: off, at: now})
	if c.ewmaN == 0 {
		c.ewma = rtt
		c.rttvar = rtt / 2
	} else {
		err := rtt - c.ewma
		c.rttvar = (1-RttVarBeta)*c.rttvar + RttVarBeta*math.Abs(err)
		c.ewma += EwmaAlpha * err
	}
	c.ewmaN++
	if c.cold {
		c.sinceCold++
		if c.sinceCold >= UnsyncedMinSamples {
			c.cold = false
		}
	}
	c.recomputeOffset(now)
	c.checkInflation(now)
}

func (c *Conn) resetModel() {
	c.app.reset()
	c.ewmaN = 0
	c.ewma, c.rttvar = 0, 0
	c.offsetOK = false
	c.stepDir, c.stepRun = 0, 0
}

// floor returns the tamper-proof lower bound on a plausible app RTT. The
// kernel floor wins when available (a pong-delaying client cannot raise it);
// the ws floor is made jitter-aware because a downward RTT lie has no payoff,
// so this is a plausibility check, not a cheat bound.
func (c *Conn) floor(now float64) (float64, bool) {
	if c.kern.ok {
		return c.kern.srtt - 2*c.kern.rttvar - 2, true
	}
	if c.ws.n >= WSFloorMinSamples {
		return c.wsMin(now) - AnchorFloorMarginMs - MADScale*c.madWS(), true
	}
	return 0, false
}

func (c *Conn) wsMin(now float64) float64 {
	m := math.Inf(1)
	all := math.Inf(1)
	for i := 0; i < c.ws.n; i++ {
		s := c.ws.newest(i)
		if s.rtt < all {
			all = s.rtt
		}
		if s.at >= now-RttMinWindowMs && s.rtt < m {
			m = s.rtt
		}
	}
	if math.IsInf(m, 1) {
		return all
	}
	return m
}

func (c *Conn) syncLie(now float64) {
	if now-c.lastSyncLie >= SyncLieMinGapMs {
		c.AddFlag(FlagSyncLie, now)
		c.lastSyncLie = now
	}
}

// recomputeOffset takes the median offset over the low-RTT samples of the last 30 s.
func (c *Conn) recomputeOffset(now float64) {
	rttMin := math.Inf(1)
	for i := 0; i < c.app.n; i++ {
		s := c.app.newest(i)
		if s.at >= now-RttMinWindowMs && s.rtt < rttMin {
			rttMin = s.rtt
		}
	}
	if math.IsInf(rttMin, 1) {
		return
	}
	thr := rttMin + math.Max(2, ResidFrac*rttMin)
	cand := c.scratchA[:0]
	for i := 0; i < c.app.n; i++ {
		s := c.app.newest(i)
		if s.at >= now-RttMinWindowMs && s.rtt <= thr {
			cand = append(cand, s.off)
		}
	}
	if len(cand) < 3 {
		// lowest-RTT quartile of the window
		type pair struct{ rtt, off float64 }
		var win []pair
		for i := 0; i < c.app.n; i++ {
			s := c.app.newest(i)
			if s.at >= now-RttMinWindowMs {
				win = append(win, pair{s.rtt, s.off})
			}
		}
		sort.Slice(win, func(i, j int) bool { return win[i].rtt < win[j].rtt })
		q := (len(win) + 3) / 4
		if q < 1 {
			q = 1
		}
		cand = cand[:0]
		for i := 0; i < q && i < len(win); i++ {
			cand = append(cand, win[i].off)
		}
	}
	c.offset = median(cand)
	c.offsetOK = true
	c.scratchA = cand[:0]
}

// OnWSPong records a WebSocket protocol ping/pong round trip measured by the server.
func (c *Conn) OnWSPong(rttMs, now float64) {
	if rttMs < 0 || rttMs > MaxSampleRttMs {
		return
	}
	c.ws.push(sample{rtt: rttMs, at: now})
	if c.wsEwmaN == 0 {
		c.wsEwma = rttMs
	} else {
		c.wsEwma += EwmaAlpha * (rttMs - c.wsEwma)
	}
	c.wsEwmaN++
	c.lastPong = now
	c.checkInflation(now)
}

// OnKernelRTT records the kernel's smoothed RTT and RTT variance for the socket.
func (c *Conn) OnKernelRTT(srttMs, rttvarMs, now float64) {
	if srttMs <= 0 {
		return
	}
	c.kern.srtt, c.kern.rttvar, c.kern.at, c.kern.ok = srttMs, math.Max(0, rttvarMs), now, true
	c.checkInflation(now)
}

// OnVisible marks the connection cold after a tab became visible again: the
// model is kept but the player is unsynced until 5 fresh samples arrive.
func (c *Conn) OnVisible(now float64) {
	c.cold = true
	c.sinceCold = 0
}

// Model returns the public model at time now.
func (c *Conn) Model(now float64) Model { return c.snap(now).Model }

func (c *Conn) snap(now float64) snapshot {
	var s snapshot
	rttMin := math.Inf(1)
	n10 := 0
	rttMax16 := 0.0
	for i := 0; i < c.app.n; i++ {
		sm := c.app.newest(i)
		if sm.at >= now-RttMinWindowMs && sm.rtt < rttMin {
			rttMin = sm.rtt
		}
		if sm.at >= now-UnsyncedWindowMs {
			n10++
		}
		if i < MADWindow && sm.rtt > rttMax16 {
			rttMax16 = sm.rtt
		}
	}
	sigmaApp := SigmaColdMs
	if c.ewmaN > 0 {
		sigmaApp = math.Max(c.rttvar, MADScale*c.madApp())
	}
	// ws anchor
	wsMax16 := 0.0
	for i := 0; i < c.ws.n && i < MADWindow; i++ {
		if r := c.ws.newest(i).rtt; r > wsMax16 {
			wsMax16 = r
		}
	}
	hasWS := c.ws.n >= 3
	anchorLo, anchorHi, sigmaAnchor := math.Inf(-1), math.Inf(1), math.Inf(1)
	anchored := false
	if c.kern.ok {
		anchorHi = c.kern.srtt + AnchorSlackKernelMs
		anchorLo = c.kern.srtt - 2*c.kern.rttvar - 2
		sigmaAnchor = c.kern.rttvar
		anchored = true
		s.RttKernelMs = c.kern.srtt
	}
	if hasWS {
		sigmaWS := MADScale * c.madWS()
		if !c.kern.ok {
			anchorHi = c.wsEwma + AnchorSlackWSMs
			anchorLo = c.wsMin(now) - AnchorFloorMarginMs - sigmaWS
		}
		sigmaAnchor = math.Min(sigmaAnchor, sigmaWS)
		anchored = true
	}
	if c.wsEwmaN > 0 {
		s.RttWsMs = c.wsEwma
	}
	var rttRef, sigma float64
	switch {
	case c.ewmaN > 0:
		rttRef, sigma = c.ewma, sigmaApp
	case hasWS:
		rttRef, sigma = c.wsEwma, SigmaColdMs
	case c.kern.ok:
		rttRef, sigma = c.kern.srtt, SigmaColdMs
	default:
		rttRef, sigma = 0, SigmaColdMs
	}
	if anchored {
		rttRef = clamp(rttRef, anchorLo, anchorHi)
		sigma = math.Min(sigma, sigmaAnchor+SigmaSlackMs)
	}
	if rttRef < 0 {
		rttRef = 0
	}
	sigma = clamp(sigma, SigmaMinMs, SigmaMaxMs)
	if math.IsInf(rttMin, 1) {
		rttMin = rttRef
	}
	resid := clamp(ResidFrac*rttMin, ResidMinMs, ResidMaxMs)
	tolCap := c.TolCapMs
	if tolCap <= 0 {
		tolCap = DefaultTolCapMs
	}
	tol := clamp(resid+3*sigma+EpsInputMs, TolMinMs, math.Max(TolMinMs, tolCap))
	u := resid + 2*sigma + 1
	lead := clamp(rttRef/2+3*sigma+LeadBaseMs, LeadMinMs, LeadMaxMs)
	q := QualityPoor
	switch {
	case c.ewmaN == 0 || n10 < UnsyncedMinSamples || c.cold:
		q = QualityUnsynced
	case rttRef < QualityGoodRttMs && sigma < QualityGoodSigmaMs:
		q = QualityGood
	case sigma < QualityFairSigmaMs:
		q = QualityFair
	}
	if wsMax16 > rttMax16 {
		rttMax16 = wsMax16
	}
	if rttMax16 == 0 {
		rttMax16 = rttRef
	}
	s.OffsetMs = c.offset
	s.RttRefMs = rttRef
	s.SigmaMs = sigma
	s.TolMs = tol
	s.UMs = u
	s.LeadMs = lead
	s.Quality = q
	s.Samples = c.app.n
	s.Flags = c.flagList()
	s.resid = resid
	s.rttMin = rttMin
	s.rttMax16 = rttMax16
	s.rttMinRef = rttMin
	if anchored && !math.IsInf(anchorLo, -1) && anchorLo > s.rttMinRef {
		s.rttMinRef = anchorLo
	}
	if s.rttMinRef > rttRef {
		s.rttMinRef = rttRef
	}
	s.sigmaApp = sigmaApp
	s.sigmaAnchor = sigmaAnchor
	s.anchorLo = anchorLo
	s.anchorHi = anchorHi
	s.anchored = anchored
	return s
}

func (c *Conn) madApp() float64 {
	xs := c.scratchA[:0]
	for i := 0; i < c.app.n && i < MADWindow; i++ {
		xs = append(xs, c.app.newest(i).rtt)
	}
	m := madRaw(xs, c.scratchB[:0])
	c.scratchA = xs[:0]
	return m
}

func (c *Conn) madWS() float64 {
	xs := c.scratchA[:0]
	for i := 0; i < c.ws.n && i < MADWindow; i++ {
		xs = append(xs, c.ws.newest(i).rtt)
	}
	m := madRaw(xs, c.scratchB[:0])
	c.scratchA = xs[:0]
	return m
}

// median sorts xs in place and returns its median (0 for empty input).
func median(xs []float64) float64 {
	n := len(xs)
	if n == 0 {
		return 0
	}
	sort.Float64s(xs)
	if n%2 == 1 {
		return xs[n/2]
	}
	return (xs[n/2-1] + xs[n/2]) / 2
}

// madRaw returns the unscaled median absolute deviation of xs (xs is sorted in place).
func madRaw(xs, scratch []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	m := median(xs)
	dev := scratch[:0]
	for _, x := range xs {
		dev = append(dev, math.Abs(x-m))
	}
	return median(dev)
}

func (c *Conn) flagList() []string {
	if len(c.flags) == 0 {
		return nil
	}
	out := make([]string, 0, len(c.flags))
	for k, v := range c.flags {
		if v > 0 {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func (c *Conn) checkInflation(now float64) {
	s := c.snap(now)
	rttInfl := s.anchored && c.ewmaN >= UnsyncedMinSamples && c.ewma > s.anchorHi
	jitInfl := !math.IsInf(s.sigmaAnchor, 1) && c.ewmaN >= UnsyncedMinSamples &&
		s.sigmaApp > JitterInflFactor*s.sigmaAnchor+JitterInflMarginMs
	wsInfl := c.kern.ok && c.ws.n >= 3 && c.wsEwma > c.kern.srtt+AnchorSlackKernelMs+5
	c.track(&c.rttInflSince, &c.rttInflFlag, rttInfl, now, FlagRttInflated)
	c.track(&c.jitInflSince, &c.jitInflFlag, jitInfl, now, FlagJitterInflated)
	c.track(&c.wsInflSince, &c.wsInflFlag, wsInfl, now, FlagRttInflated)
}

func (c *Conn) track(since *float64, flagged *bool, cond bool, now float64, flag string) {
	if !cond {
		*since = -1
		*flagged = false
		return
	}
	if *since < 0 {
		*since = now
		return
	}
	if !*flagged && now-*since >= InflationWindowMs {
		c.AddFlag(flag, now)
		*flagged = true
	}
}

// AddFlag records an integrity flag; scoring flags cost one trust point and
// reset the clean streak.
func (c *Conn) AddFlag(flag string, now float64) {
	c.flags[flag]++
	if scoringFlags[flag] {
		c.score--
		c.cleanStreak = 0
	}
}

// Trust returns the ladder state.
func (c *Conn) Trust() Trust {
	t := Trust{Score: c.score, Level: TrustFull}
	if len(c.flags) > 0 {
		t.Flags = make(map[string]int, len(c.flags))
		for k, v := range c.flags {
			t.Flags[k] = v
		}
	}
	if c.ForceLevel != "" {
		t.Level = c.ForceLevel
		return t
	}
	if !c.AutoTrust {
		return t
	}
	t1, t2, t3 := ThresholdConservative, ThresholdServerOnly, ThresholdArrivalOnly
	if c.StrictTrust {
		t1, t2, t3 = ThresholdStrictConservative, ThresholdStrictServerOnly, ThresholdStrictArrivalOnly
	}
	switch {
	case c.score <= t3:
		t.Level = TrustArrivalOnly
	case c.score <= t2:
		t.Level = TrustServerOnly
	case c.score <= t1:
		t.Level = TrustConservative
	}
	return t
}

// LockedUntil returns the end of the current false-start lockout (server ms).
func (c *Conn) LockedUntil() float64 { return c.lockedUntil }

// lockout applies a false-start lockout with growth for repeats within 10 s.
func (c *Conn) lockout(now, baseMs, growth, capMs float64) float64 {
	k := 0
	for i := 0; i < c.falseStarts.n; i++ {
		if c.falseStarts.newest(i) >= now-LockoutRepeatWindowMs {
			k++
		}
	}
	d := baseMs
	for i := 0; i < k; i++ {
		d *= growth
	}
	if capMs > 0 && d > capMs {
		d = capMs
	}
	until := now + d
	if until > c.lockedUntil {
		c.lockedUntil = until
	}
	c.falseStarts.push(now)
	return c.lockedUntil
}

// notePress enforces the PRESS rate limit; false means the message is dropped.
func (c *Conn) notePress(now float64) bool {
	c.pressTimes.push(now)
	cnt := 0
	for i := 0; i < c.pressTimes.n; i++ {
		if c.pressTimes.newest(i) >= now-RateLimitWindowMs {
			cnt++
		}
	}
	if cnt > RateLimitPresses {
		if now-c.lastRateFlag >= RateLimitWindowMs {
			c.AddFlag(FlagRateLimit, now)
			c.lastRateFlag = now
		}
		return false
	}
	return true
}

func (c *Conn) softFlag(flag string) { c.flags[flag]++ }

// noteReaction records an accepted press for the streak, clamp-ratio and bot detectors.
func (c *Conn) noteReaction(reaction float64, clean, clamped bool, now float64) (raised []string) {
	c.presses++
	c.react.push(reaction)
	c.clampHist.push(clamped)
	// No recovery while a sustained detector (bias, clamp ratio, bot) is active:
	// a shaved-within-tol press carries no per-press flag, so the streak would
	// otherwise forgive one steal per five presses forever.
	if clean && !c.biasActive && !c.clampFlag && !c.botFlag {
		c.cleanStreak++
		if c.cleanStreak >= TrustCleanStreak {
			c.cleanStreak = 0
			if c.score < 0 {
				c.score++
			}
		}
	}
	// clamp ratio
	if c.clampHist.n >= ClampRatioMinPresses {
		k := 0
		for i := 0; i < c.clampHist.n; i++ {
			if c.clampHist.newest(i) {
				k++
			}
		}
		if float64(k)/float64(c.clampHist.n) > ClampRatio {
			if !c.clampFlag || c.presses-c.clampFlagAt >= ClampRatioRepeat {
				c.AddFlag(FlagClampRatio, now)
				c.clampFlag = true
				c.clampFlagAt = c.presses
				raised = append(raised, FlagClampRatio)
			}
		} else {
			c.clampFlag = false
		}
	}
	// bot-like
	if c.presses >= BotMinPresses {
		xs := c.scratchA[:0]
		for i := 0; i < c.react.n; i++ {
			xs = append(xs, c.react.newest(i))
		}
		med := median(xs)
		mad := madRaw(xs, c.scratchB[:0])
		c.scratchA = xs[:0]
		if med < BotMedianMs && mad < BotMADMs {
			if !c.botFlag || c.presses-c.botFlagAt >= BotRepeat {
				c.AddFlag(FlagBotLike, now)
				c.botFlag = true
				c.botFlagAt = c.presses
				raised = append(raised, FlagBotLike)
			}
		} else {
			c.botFlag = false
		}
	}
	return raised
}

// noteBias feeds the early-bias detector with tClient − tArr.
func (c *Conn) noteBias(v float64, m snapshot, now float64) bool {
	c.bias.push(v)
	if c.bias.n < EarlyBiasWindow {
		return false
	}
	sum := 0.0
	for i := 0; i < c.bias.n; i++ {
		sum += c.bias.newest(i)
	}
	if sum/float64(c.bias.n) < -(m.SigmaMs + m.resid + EpsInputMs) {
		c.AddFlag(FlagEarlyBias, now)
		c.bias.reset()
		return true
	}
	return false
}

// noteAck feeds the ack_late detector.
func (c *Conn) noteAck(excess, allow, sigma, now float64) {
	c.ackLate.push(excess > allow+2*sigma+5) // honest spikes must not count
	k := 0
	for i := 0; i < c.ackLate.n; i++ {
		if c.ackLate.newest(i) {
			k++
		}
	}
	if k >= AckLateCount {
		if !c.ackLateFlag {
			c.AddFlag(FlagAckLate, now)
			c.ackLateFlag = true
		}
	} else {
		c.ackLateFlag = false
	}
}
