package packs

// Summary is the list/metadata view of a pack, cheap to load without the
// full document.
type Summary struct {
	ID            string   `json:"id" doc:"UUIDv7"`
	Version       int      `json:"version" doc:"Optimistic-concurrency version"`
	Name          string   `json:"name"`
	Language      string   `json:"language,omitempty" doc:"BCP-47 tag, e.g. ru-RU"`
	Difficulty    int      `json:"difficulty" doc:"0..10"`
	Restriction   string   `json:"restriction,omitempty" doc:"Age restriction, e.g. 18+"`
	Tags          []string `json:"tags"`
	Authors       []string `json:"authors" doc:"Pack-level authors"`
	RoundCount    int      `json:"roundCount"`
	ThemeCount    int      `json:"themeCount"`
	QuestionCount int      `json:"questionCount" doc:"Playable questions (empty slots with price -1 are not counted)"`
	HasMedia      bool     `json:"hasMedia" doc:"True when any question, answer or option contains a non-text item (stored or external)"`
	LogoMediaID   string   `json:"logoMediaId,omitempty" doc:"Media ID of the logo image"`
	CreatedAt     int64    `json:"createdAt" doc:"Unix ms UTC"`
	UpdatedAt     int64    `json:"updatedAt" doc:"Unix ms UTC"`
}

// Summarize derives the Summary of a pack. Slices are copied and never nil.
func Summarize(p *Pack) Summary {
	s := Summary{
		ID:          p.ID,
		Version:     p.Version,
		Name:        p.Name,
		Language:    p.Language,
		Difficulty:  p.Difficulty,
		Restriction: p.Restriction,
		Tags:        append([]string{}, p.Tags...),
		Authors:     append([]string{}, p.Info.Authors...),
		LogoMediaID: p.LogoMediaID,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
	s.RoundCount = len(p.Rounds)
	for _, r := range p.Rounds {
		s.ThemeCount += len(r.Themes)
		for _, t := range r.Themes {
			for _, q := range t.Questions {
				if !q.IsEmpty() {
					s.QuestionCount++
				}
				if !s.HasMedia && questionHasMedia(&q) {
					s.HasMedia = true
				}
			}
		}
	}
	return s
}

func questionHasMedia(q *Question) bool {
	if itemsHaveMedia(q.Params.Question) || itemsHaveMedia(q.Params.Answer) {
		return true
	}
	for _, o := range q.Params.AnswerOptions {
		if itemsHaveMedia(o.Content) {
			return true
		}
	}
	return false
}

func itemsHaveMedia(items []ContentItem) bool {
	for _, it := range items {
		if it.IsMedia() {
			return true
		}
	}
	return false
}
