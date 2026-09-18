package clock

import (
	"sort"
	"sync"
	"time"
)

// Fake is a manually advanced clock for tests. Advance fires pending After
// channels whose deadline has passed, in deadline order.
type Fake struct {
	mu      sync.Mutex
	now     time.Time
	mono    float64
	waiters []fakeWaiter
}

type fakeWaiter struct {
	at float64
	ch chan time.Time
}

// NewFake returns a Fake clock starting at the given wall time and Mono()==0.
func NewFake(start time.Time) *Fake { return &Fake{now: start} }

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *Fake) Mono() float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mono
}

func (f *Fake) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.waiters = append(f.waiters, fakeWaiter{at: f.mono + float64(d.Nanoseconds())/1e6, ch: ch})
	return ch
}

// Advance moves the clock forward by d and fires due waiters.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	f.mono += float64(d.Nanoseconds()) / 1e6
	f.now = f.now.Add(d)
	sort.SliceStable(f.waiters, func(i, j int) bool { return f.waiters[i].at < f.waiters[j].at })
	var due []fakeWaiter
	rest := f.waiters[:0]
	for _, w := range f.waiters {
		if w.at <= f.mono {
			due = append(due, w)
		} else {
			rest = append(rest, w)
		}
	}
	f.waiters = rest
	now := f.now
	f.mu.Unlock()
	for _, w := range due {
		w.ch <- now
	}
}

// Set moves Mono to an absolute value (must not go backwards).
func (f *Fake) Set(monoMs float64) {
	f.mu.Lock()
	d := monoMs - f.mono
	f.mu.Unlock()
	if d > 0 {
		f.Advance(time.Duration(d * float64(time.Millisecond)))
	}
}
