package buzzer

import (
	"math"
	"math/rand"
)

// feedOpts describes a synthetic link for unit tests.
type feedOpts struct {
	rtt, sigma, offset float64
	n                  int
	intervalMs         float64
	wsRtt              float64             // > 0: feed one pong per sample with this base RTT
	c4Delay            func(i int) float64 // attacker: extra delay reported on c4
}

// feed drives c with n interleaved SYNC samples starting at t0 (server ms)
// and returns the server time after the last exchange.
func feed(c *Conn, t0 float64, o feedOpts, rng *rand.Rand) float64 {
	if o.intervalMs == 0 {
		o.intervalMs = SyncBurstIntervalMs
	}
	t := t0
	var prevSeq int64
	var prevC4 float64
	hasPrev := false
	for i := 0; i < o.n; i++ {
		j1, j2 := 0.0, 0.0
		if o.sigma > 0 {
			j1 = rng.NormFloat64() * o.sigma / math.Sqrt2
			j2 = rng.NormFloat64() * o.sigma / math.Sqrt2
		}
		up := math.Max(0.2, o.rtt/2+j1)
		down := math.Max(0.2, o.rtt/2+j2)
		c1 := t - o.offset
		s2 := t + up
		s3 := s2 + 0.05
		out := c.OnSync(SyncIn{Seq: int64(i) + 1, C1: c1, PrevSeq: prevSeq, PrevC4: prevC4, HasPrev: hasPrev}, s2, s3)
		c4 := s3 + down - o.offset
		if o.c4Delay != nil {
			c4 += o.c4Delay(i)
		}
		prevSeq, prevC4, hasPrev = out.Seq, c4, true
		if o.wsRtt > 0 {
			wr := o.wsRtt
			if o.sigma > 0 {
				wr += rng.NormFloat64() * o.sigma
			}
			c.OnWSPong(math.Max(0.3, wr), s3+0.1)
		}
		t += o.intervalMs
	}
	// flush the last c4 with one more SYNC (its own c4 stays in flight)
	c.OnSync(SyncIn{Seq: int64(o.n) + 1, C1: t - o.offset, PrevSeq: prevSeq, PrevC4: prevC4, HasPrev: hasPrev}, t+o.rtt/2, t+o.rtt/2+0.05)
	return t + o.rtt + 1
}

// syncedConn returns a connection synced on a symmetric link with 20 samples.
func syncedConn(t0, rtt, sigma, offset float64, rng *rand.Rand) (*Conn, float64) {
	c := NewConn(t0)
	end := feed(c, t0, feedOpts{rtt: rtt, sigma: sigma, offset: offset, n: 20, wsRtt: rtt}, rng)
	return c, end
}

func hasFlag(c *Conn, flag string) bool { return c.Trust().Flags[flag] > 0 }
