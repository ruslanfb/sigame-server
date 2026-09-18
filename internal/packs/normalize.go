package packs

import "strings"

// DefaultDifficulty is assigned to a never-persisted pack whose difficulty is
// left at 0 (SIGame's own default).
const DefaultDifficulty = 5

// Normalize brings a pack into canonical form before validation and storage:
//
//   - names and short metadata strings are trimmed; tags, authors and sources
//     are trimmed and empty entries dropped;
//   - an empty round type becomes "standard", an empty question type "simple";
//   - nil slices that are always serialised (tags, rounds, themes, questions,
//     params.question, right, answer option content, script step params)
//     become empty slices so that JSON contains [] rather than null;
//   - Right gets a single empty entry when it is empty (an answer slot the
//     showman fills in); entries are trimmed;
//   - Difficulty 0 on a never-persisted pack (Version == 0 && CreatedAt == 0)
//     means "unset" and becomes DefaultDifficulty. On a persisted pack 0 is an
//     explicit value and is kept. Repo.Update copies the stored Version and
//     CreatedAt onto the pack before normalising, so updates never trigger
//     the default;
//   - media ids are lower-cased, URLs trimmed;
//   - answer option labels are trimmed and upper-cased (A, B, C ...);
//   - audio items with an empty placement stay empty: ContentItem.
//     EffectivePlacement applies the SIQ default (background) at play time.
func Normalize(p *Pack) {
	if p == nil {
		return
	}
	p.Name = strings.TrimSpace(p.Name)
	p.Language = strings.TrimSpace(p.Language)
	p.Restriction = strings.TrimSpace(p.Restriction)
	p.Date = strings.TrimSpace(p.Date)
	p.Publisher = strings.TrimSpace(p.Publisher)
	p.ContactURI = strings.TrimSpace(p.ContactURI)
	p.LogoMediaID = strings.ToLower(strings.TrimSpace(p.LogoMediaID))
	p.LogoURL = strings.TrimSpace(p.LogoURL)
	p.Tags = cleanList(p.Tags)
	if p.Difficulty == 0 && p.Version == 0 && p.CreatedAt == 0 {
		p.Difficulty = DefaultDifficulty
	}
	normalizeInfo(&p.Info)
	if p.Rounds == nil {
		p.Rounds = []Round{}
	}
	for i := range p.Rounds {
		normalizeRound(&p.Rounds[i])
	}
}

func normalizeRound(r *Round) {
	r.Name = strings.TrimSpace(r.Name)
	if r.Type == "" {
		r.Type = RoundStandard
	}
	normalizeInfo(&r.Info)
	if r.Themes == nil {
		r.Themes = []Theme{}
	}
	for i := range r.Themes {
		normalizeTheme(&r.Themes[i])
	}
}

func normalizeTheme(t *Theme) {
	t.Name = strings.TrimSpace(t.Name)
	normalizeInfo(&t.Info)
	if t.Questions == nil {
		t.Questions = []Question{}
	}
	for i := range t.Questions {
		normalizeQuestion(&t.Questions[i])
	}
}

func normalizeQuestion(q *Question) {
	q.Type = QuestionType(strings.TrimSpace(string(q.Type)))
	if q.Type == "" {
		q.Type = QSimple
	}
	normalizeInfo(&q.Info)
	if q.Params.Question == nil {
		q.Params.Question = []ContentItem{}
	}
	normalizeItems(q.Params.Question)
	normalizeItems(q.Params.Answer)
	q.Params.Theme = strings.TrimSpace(q.Params.Theme)
	for i := range q.Params.AnswerOptions {
		o := &q.Params.AnswerOptions[i]
		o.Label = strings.ToUpper(strings.TrimSpace(o.Label))
		if o.Content == nil {
			o.Content = []ContentItem{}
		}
		normalizeItems(o.Content)
	}
	for i := range q.Right {
		q.Right[i] = strings.TrimSpace(q.Right[i])
	}
	if len(q.Right) == 0 {
		q.Right = []string{""}
	}
	for i := range q.Wrong {
		q.Wrong[i] = strings.TrimSpace(q.Wrong[i])
	}
	for i := range q.Script {
		if q.Script[i].Params == nil {
			q.Script[i].Params = []Param{}
		}
		normalizeParams(q.Script[i].Params)
	}
	normalizeParams(q.Extra)
}

func normalizeParams(params []Param) {
	for i := range params {
		normalizeItems(params[i].Items)
		normalizeParams(params[i].Params)
	}
}

func normalizeItems(items []ContentItem) {
	for i := range items {
		it := &items[i]
		it.MediaID = strings.ToLower(strings.TrimSpace(it.MediaID))
		it.URL = strings.TrimSpace(it.URL)
	}
}

func normalizeInfo(in *Info) {
	in.Authors = cleanListKeepNil(in.Authors)
	in.Sources = cleanListKeepNil(in.Sources)
}

// cleanList trims entries, drops empty ones and never returns nil.
func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// cleanListKeepNil is cleanList for omitempty fields: nil stays nil.
func cleanListKeepNil(in []string) []string {
	if in == nil {
		return nil
	}
	return cleanList(in)
}
