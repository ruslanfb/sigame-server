// Package clock abstracts wall-clock and monotonic time so that the engine,
// buzzer and room actor are deterministic under test.
package clock

import "time"

// Clock provides time to the game runtime.
type Clock interface {
	// Now is the wall clock (used for timestamps sent to clients).
	Now() time.Time
	// Mono returns monotonic milliseconds since an arbitrary origin. All buzzer
	// timing and timer deadlines use this clock; it never jumps.
	Mono() float64
	// After fires once after d (like time.After).
	After(d time.Duration) <-chan time.Time
}

// NowMs returns the wall clock as Unix milliseconds.
func NowMs(c Clock) int64 { return c.Now().UnixMilli() }

// Real is the production clock.
type Real struct{ origin time.Time }

// NewReal returns a Real clock whose Mono origin is now.
func NewReal() *Real { return &Real{origin: time.Now()} }

func (r *Real) Now() time.Time { return time.Now() }

func (r *Real) Mono() float64 {
	return float64(time.Since(r.origin).Nanoseconds()) / 1e6
}

func (r *Real) After(d time.Duration) <-chan time.Time { return time.After(d) }
