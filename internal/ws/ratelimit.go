package ws

// Limiter is a token bucket over the monotonic clock (ms). It is not
// goroutine-safe; each connection's reader owns one.
type Limiter struct {
	rate   float64 // tokens per ms
	burst  float64
	tokens float64
	last   float64
}

// NewLimiter allows perSecond messages sustained with a burst of burst.
func NewLimiter(perSecond int, burst int, now float64) *Limiter {
	if perSecond <= 0 {
		perSecond = 1
	}
	if burst <= 0 {
		burst = perSecond
	}
	return &Limiter{rate: float64(perSecond) / 1000, burst: float64(burst), tokens: float64(burst), last: now}
}

// Allow consumes one token if available.
func (l *Limiter) Allow(now float64) bool {
	if now > l.last {
		l.tokens += (now - l.last) * l.rate
		if l.tokens > l.burst {
			l.tokens = l.burst
		}
		l.last = now
	}
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}
