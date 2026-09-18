package room

import (
	"context"
	"time"

	"sigame/internal/engine"
)

// Engine timers are real time.AfterFunc timers that post a TIMEOUT command
// to the mailbox; only the AtMs stamps come from clock.Clock. Deadlines are
// therefore wall-clock accurate to the scheduler resolution, and a fake
// clock in tests only affects the stamps the engine sees, not the firing.

// handleTimerEvent schedules / cancels the timer described by a timer event
// and returns the event to forward (mediaFallback gets its real duration).
func (r *Room) handleTimerEvent(e engine.Event) engine.Event {
	switch e.Type {
	case engine.EvTimerStart:
		p, ok := e.Payload.(engine.TimerStartPayload)
		if !ok {
			return e
		}
		if p.Kind == engine.TimerMediaFallback {
			p.DurationMs = r.mediaFallbackMs(p.DurationMs)
			e.Payload = p
		}
		r.scheduleTimer(p.ID, p.DurationMs)
	case engine.EvTimerStop:
		if p, ok := e.Payload.(engine.TimerStopPayload); ok {
			r.stopTimer(p.ID)
		}
	case engine.EvTimerPause:
		if p, ok := e.Payload.(engine.TimerPausePayload); ok {
			r.stopTimer(p.ID)
		}
	case engine.EvTimerResume:
		if p, ok := e.Payload.(engine.TimerPausePayload); ok {
			r.scheduleTimer(p.ID, p.RemainingMs)
		}
	}
	return e
}

func (r *Room) scheduleTimer(id string, ms int64) {
	r.stopTimer(id)
	if ms < 0 {
		ms = 0
	}
	r.timers[id] = r.after(time.Duration(ms)*time.Millisecond, func() { r.onTimeout(id) })
}

func (r *Room) stopTimer(id string) {
	if t, ok := r.timers[id]; ok {
		t.Stop()
		delete(r.timers, id)
	}
}

func (r *Room) onTimeout(id string) {
	if _, ok := r.timers[id]; !ok {
		return // cancelled after firing
	}
	delete(r.timers, id)
	if r.game == nil || r.closing {
		return
	}
	if err := r.apply(engine.Command{Type: engine.CmdTimeout, Actor: systemActor, TimerID: id}); err != nil {
		r.log.Warn("room: timeout rejected", "room", r.code, "timer", id, "err", err)
	}
}

// mediaFallbackMs picks the duration of a mediaFallback timer: the maximum of
// the engine's known minimum, the probed duration of the current content
// group's media (media.Store.Stat) and, when nothing is known, the
// configured maximum.
func (r *Room) mediaFallbackMs(known int64) int64 {
	best := known
	probed := false
	if r.game != nil && r.m.deps.Media != nil {
		if q := r.game.State().Question; q != nil && q.Cursor >= 0 && q.Cursor < len(q.Groups) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			for _, it := range q.Groups[q.Cursor].Items {
				if it.MediaID == "" {
					continue
				}
				meta, err := r.m.deps.Media.Stat(ctx, it.MediaID)
				if err != nil || meta.DurationMs <= 0 {
					continue
				}
				probed = true
				if meta.DurationMs > best {
					best = meta.DurationMs
				}
			}
		}
	}
	if !probed && best <= 0 {
		best = r.m.deps.Cfg.MediaFallbackMaxMs
	}
	if best <= 0 {
		best = defaultMediaFallbackMs
	}
	return best
}
