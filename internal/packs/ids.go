package packs

import "github.com/google/uuid"

// NewID returns a new UUIDv7 string (time-ordered, so packs list naturally by
// creation time when sorted by id).
func NewID() string { return uuid.Must(uuid.NewV7()).String() }

// EnsureIDs fills every empty ID of the pack, its rounds, themes and questions
// with a fresh UUIDv7. Existing IDs are kept.
func EnsureIDs(p *Pack) {
	if p == nil {
		return
	}
	if p.ID == "" {
		p.ID = NewID()
	}
	for i := range p.Rounds {
		r := &p.Rounds[i]
		if r.ID == "" {
			r.ID = NewID()
		}
		for j := range r.Themes {
			t := &r.Themes[j]
			if t.ID == "" {
				t.ID = NewID()
			}
			for k := range t.Questions {
				q := &t.Questions[k]
				if q.ID == "" {
					q.ID = NewID()
				}
			}
		}
	}
}

// ResetIDs clears every ID of the pack, its rounds, themes and questions so
// that EnsureIDs assigns fresh ones (used by Duplicate).
func ResetIDs(p *Pack) {
	if p == nil {
		return
	}
	p.ID = ""
	for i := range p.Rounds {
		r := &p.Rounds[i]
		r.ID = ""
		for j := range r.Themes {
			t := &r.Themes[j]
			t.ID = ""
			for k := range t.Questions {
				t.Questions[k].ID = ""
			}
		}
	}
}
