package buzzer

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOffsetFromFourTuple(t *testing.T) {
	c := NewConn(0)
	end := feed(c, 0, feedOpts{rtt: 10, offset: 12345.678, n: 10, wsRtt: 10}, rand.New(rand.NewSource(1)))
	m := c.Model(end)
	require.InDelta(t, 12345.678, m.OffsetMs, 1e-6)
	require.InDelta(t, 10, m.RttRefMs, 0.2)
	require.Equal(t, QualityGood, m.Quality)
	require.Equal(t, 10, m.Samples)
	require.InDelta(t, 10, m.RttWsMs, 0.01)
	// tol/u/lead derive from resid=2, σ floor 0.5..: tol = 2 + 3σ + 2 ≥ 5
	require.GreaterOrEqual(t, m.TolMs, TolMinMs)
	require.Less(t, m.TolMs, 8.0)
	require.InDelta(t, m.RttRefMs/2+3*m.SigmaMs+LeadBaseMs, m.LeadMs, 1e-9)
}

func TestRttFilteringAndSigma(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	c := NewConn(0)
	end := feed(c, 0, feedOpts{rtt: 60, sigma: 8, offset: -500, n: 40, wsRtt: 60}, rng)
	m := c.Model(end)
	require.InDelta(t, 60, m.RttRefMs, 10)
	require.InDelta(t, -500, m.OffsetMs, 4)
	require.Greater(t, m.SigmaMs, 3.0)
	require.Less(t, m.SigmaMs, 16.0)
	require.Equal(t, QualityFair, m.Quality)
	require.InDelta(t, clamp(0.1*60, 2, 10)+3*m.SigmaMs+2, m.TolMs, 2.5) // resid ≈ 6 (rtt_min slightly below 60)
	require.InDelta(t, m.TolMs-m.SigmaMs-1, m.UMs, 1e-9)
}

func TestWsFloorRejectsSyncLie(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	c := NewConn(0)
	end := feed(c, 0, feedOpts{rtt: 60, sigma: 1, offset: 0, n: 20, wsRtt: 60}, rng)
	before := c.Model(end).Samples
	// The client now claims 30 ms round trips while the transport says 60.
	end2 := feed(c, end, feedOpts{rtt: 30, sigma: 0, offset: 0, n: 5}, rng)
	m := c.Model(end2)
	require.Equal(t, before, m.Samples, "lying samples must be discarded")
	require.True(t, hasFlag(c, FlagSyncLie))
	require.Equal(t, 1, c.Trust().Flags[FlagSyncLie], "charged at most once per 2 s")
	require.Equal(t, -1, c.Trust().Score)
	require.InDelta(t, 60, m.RttRefMs, 3)
}

func TestSigmaCeilingFromAnchor(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	honest := NewConn(0)
	feed(honest, 0, feedOpts{rtt: 60, sigma: 2, offset: 0, n: 80, wsRtt: 60}, rand.New(rand.NewSource(11)))
	cheat := NewConn(0)
	end := feed(cheat, 0, feedOpts{rtt: 60, sigma: 2, offset: 0, n: 80, wsRtt: 60, c4Delay: func(i int) float64 {
		if i%2 == 1 {
			return 200 // judge 3's attack: delay every second SYNC reply
		}
		return 0
	}}, rng)
	mh, mc := honest.Model(end), cheat.Model(end)
	require.Less(t, mc.SigmaMs, 8.0, "σ must be capped by the ws anchor, got %v", mc.SigmaMs)
	require.InDelta(t, mh.TolMs, mc.TolMs, 3*SigmaSlackMs+2, "tol may widen by at most 3·slack")
	require.True(t, hasFlag(cheat, FlagJitterInflated))
	require.True(t, hasFlag(cheat, FlagRttInflated))
	require.LessOrEqual(t, mc.RttRefMs, 60+AnchorSlackWSMs+2)
	// the offset still comes from the honest (low-RTT) half
	require.InDelta(t, 0, mc.OffsetMs, 3)
}

func TestRttRefClampAndInflation(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	c := NewConn(0)
	end := feed(c, 0, feedOpts{rtt: 60, sigma: 1, offset: 0, n: 80, wsRtt: 60, c4Delay: func(int) float64 { return 60 }}, rng)
	m := c.Model(end)
	require.LessOrEqual(t, m.RttRefMs, 60+AnchorSlackWSMs+1)
	require.True(t, hasFlag(c, FlagRttInflated))
	// kernel anchor tightens the ceiling further
	c.OnKernelRTT(60, 1, end)
	m = c.Model(end)
	require.LessOrEqual(t, m.RttRefMs, 60+AnchorSlackKernelMs+0.1)
	require.InDelta(t, 60, m.RttKernelMs, 1e-9)
	// a delayed pong (non-browser client) is caught by the kernel anchor
	c2 := NewConn(0)
	end = feed(c2, 0, feedOpts{rtt: 60, sigma: 1, offset: 0, n: 30, wsRtt: 120}, rng) // app 60 < ws floor → sync_lie
	require.True(t, hasFlag(c2, FlagSyncLie))
	c3 := NewConn(0)
	for i := 0; i < 20; i++ {
		c3.OnKernelRTT(60, 1, float64(i)*500)
		c3.OnWSPong(120, float64(i)*500)
	}
	require.True(t, hasFlag(c3, FlagRttInflated))
}

func TestQualityTransitions(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	c := NewConn(0)
	require.Equal(t, QualityUnsynced, c.Model(0).Quality)
	end := feed(c, 0, feedOpts{rtt: 8, sigma: 0.5, n: 4, wsRtt: 8}, rng)
	require.Equal(t, QualityUnsynced, c.Model(end).Quality, "4 samples are not enough")
	end = feed(c, end, feedOpts{rtt: 8, sigma: 0.5, n: 6, wsRtt: 8}, rng)
	require.Equal(t, QualityGood, c.Model(end).Quality)
	c.OnVisible(end)
	require.Equal(t, QualityUnsynced, c.Model(end).Quality)
	end = feed(c, end, feedOpts{rtt: 8, sigma: 0.5, n: 5, wsRtt: 8}, rng)
	require.Equal(t, QualityGood, c.Model(end).Quality)
	require.Equal(t, QualityUnsynced, c.Model(end+UnsyncedWindowMs+1).Quality, "no samples for 10 s")

	w := NewConn(0)
	end = feed(w, 0, feedOpts{rtt: 60, sigma: 8, n: 30, wsRtt: 60}, rng)
	require.Equal(t, QualityFair, w.Model(end).Quality)
	p := NewConn(0)
	end = feed(p, 0, feedOpts{rtt: 250, sigma: 28, n: 40, wsRtt: 250}, rng)
	require.Equal(t, QualityPoor, p.Model(end).Quality)
	require.LessOrEqual(t, p.Model(end).SigmaMs, SigmaMaxMs)
}

func TestStepDetectionResetsModel(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	c := NewConn(0)
	end := feed(c, 0, feedOpts{rtt: 10, sigma: 0.5, offset: 1000, n: 20, wsRtt: 10}, rng)
	require.InDelta(t, 1000, c.Model(end).OffsetMs, 1)
	end = feed(c, end, feedOpts{rtt: 10, sigma: 0.5, offset: 1100, n: 3, wsRtt: 10}, rng)
	m := c.Model(end)
	require.Equal(t, QualityUnsynced, m.Quality, "3 consistent deviations reset the ring")
	require.LessOrEqual(t, m.Samples, 3)
	end = feed(c, end, feedOpts{rtt: 10, sigma: 0.5, offset: 1100, n: 6, wsRtt: 10}, rng)
	require.InDelta(t, 1100, c.Model(end).OffsetMs, 1)
	require.Equal(t, QualityGood, c.Model(end).Quality)
}

func TestInterleavedSyncToleratesOutOfOrderAcks(t *testing.T) {
	c := NewConn(0)
	// Four SYNCs in flight (WAN burst), c4 of seq 1 reported only in seq 5.
	c.OnSync(SyncIn{Seq: 1, C1: 0}, 125, 125.05)
	c.OnSync(SyncIn{Seq: 2, C1: 100}, 225, 225.05)
	c.OnSync(SyncIn{Seq: 3, C1: 200}, 325, 325.05)
	c.OnSync(SyncIn{Seq: 4, C1: 300}, 425, 425.05)
	out := c.OnSync(SyncIn{Seq: 5, C1: 400, PrevSeq: 1, PrevC4: 250.05, HasPrev: true}, 525, 525.05)
	require.Equal(t, 1, out.Model.Samples)
	require.InDelta(t, 250, out.Model.RttRefMs, 0.1)
	// a c4 for an unknown seq is ignored
	out = c.OnSync(SyncIn{Seq: 6, C1: 500, PrevSeq: 99, PrevC4: 1, HasPrev: true}, 625, 625.05)
	require.Equal(t, 1, out.Model.Samples)
	// negative round trips are impossible
	c.OnSync(SyncIn{Seq: 7, C1: 600, PrevSeq: 6, PrevC4: 500, HasPrev: true}, 725, 725.05)
	require.True(t, hasFlag(c, FlagSyncLie))
}

func TestTrustLadder(t *testing.T) {
	c := NewConn(0)
	require.Equal(t, TrustFull, c.Trust().Level)
	c.AddFlag(FlagEarlyBias, 0)
	require.Equal(t, TrustFull, c.Trust().Level)
	c.AddFlag(FlagEarlyBias, 0)
	require.Equal(t, TrustConservative, c.Trust().Level)
	c.AddFlag(FlagSyncLie, 0)
	c.AddFlag(FlagSyncLie, 0)
	require.Equal(t, TrustServerOnly, c.Trust().Level)
	c.AddFlag(FlagTooEarly, 0)
	c.AddFlag(FlagTooEarly, 0)
	require.Equal(t, TrustArrivalOnly, c.Trust().Level)
	require.Equal(t, -6, c.Trust().Score)
	// soft flags never change the score
	c.AddFlag(FlagStale, 0)
	c.AddFlag(FlagDuplicate, 0)
	require.Equal(t, -6, c.Trust().Score)
	// 5 clean presses → +1
	for i := 0; i < 5; i++ {
		c.noteReaction(250, true, false, float64(i))
	}
	require.Equal(t, -5, c.Trust().Score)
	require.Equal(t, TrustServerOnly, c.Trust().Level)
	for i := 0; i < 100; i++ {
		c.noteReaction(250+float64(i%7)*10, true, false, float64(i))
	}
	require.Equal(t, 0, c.Trust().Score, "score never goes above 0")
	require.Equal(t, TrustFull, c.Trust().Level)
	// strict ladder and host controls
	c.StrictTrust = true
	c.AddFlag(FlagEarlyBias, 0)
	require.Equal(t, TrustConservative, c.Trust().Level)
	c.AutoTrust = false
	require.Equal(t, TrustFull, c.Trust().Level)
	c.ForceLevel = TrustArrivalOnly
	require.Equal(t, TrustArrivalOnly, c.Trust().Level)
}

func TestBotDetectorAndRateLimit(t *testing.T) {
	c := NewConn(0)
	for i := 0; i < 8; i++ {
		c.noteReaction(110+float64(i)*10, true, false, float64(i)*1000)
	}
	require.True(t, hasFlag(c, FlagBotLike))
	h := NewConn(0)
	for i := 0; i < 20; i++ {
		h.noteReaction(230+float64((i*37)%80), true, false, float64(i)*1000)
	}
	require.False(t, hasFlag(h, FlagBotLike))
	r := NewConn(0)
	for i := 0; i < RateLimitPresses; i++ {
		require.True(t, r.notePress(float64(i)*10))
	}
	require.False(t, r.notePress(60))
	require.True(t, hasFlag(r, FlagRateLimit))
	require.True(t, r.notePress(2000))
}

func TestClampRatioChargesTrustOnlyWhenSustained(t *testing.T) {
	c := NewConn(0)
	c.noteReaction(250, false, true, 0) // one honest spike
	require.Equal(t, 0, c.Trust().Score)
	for i := 1; i < 10; i++ {
		c.noteReaction(250, i%2 == 0, i%2 == 1, float64(i))
	}
	require.True(t, hasFlag(c, FlagClampRatio))
	require.Equal(t, -1, c.Trust().Score)
	require.False(t, math.IsNaN(float64(c.Trust().Score)))
}
