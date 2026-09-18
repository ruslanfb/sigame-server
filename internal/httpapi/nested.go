package httpapi

import (
	"context"
	"errors"
	"net/http"
	"reflect"

	"github.com/danielgtaylor/huma/v2"

	"sigame/internal/packs"
)

// Nested editing endpoints modify one round/theme/question of a pack. Every
// operation loads the pack, applies the change in memory and stores it
// through Repo.Update so that normalisation, validation and media reference
// counting stay in the repository. The full updated pack is returned and its
// version is incremented; concurrency works like PUT /packs/{id} via
// If-Match (the version read at the start of the operation is used when the
// header is absent, so concurrent edits never overwrite each other).

// optionalInt is an integer query parameter that remembers whether it was
// sent. A plain int with a `default` tag cannot express "0 was sent": huma
// applies defaults to every zero-valued field after parsing.
type optionalInt struct {
	Value int
	IsSet bool
}

// Schema implements huma.SchemaProvider: documented as a plain integer.
func (optionalInt) Schema(r huma.Registry) *huma.Schema {
	return huma.SchemaFromType(r, reflect.TypeOf(0))
}

// Receiver implements huma.ParamWrapper.
func (o *optionalInt) Receiver() reflect.Value { return reflect.ValueOf(o).Elem().Field(0) }

// OnParamSet implements huma.ParamReactor.
func (o *optionalInt) OnParamSet(isSet bool, _ any) { o.IsSet = isSet }

// position returns the insert index: -1 (append) when the parameter is absent.
func (o optionalInt) position() int {
	if !o.IsSet {
		return -1
	}
	return o.Value
}

const nestedConcDesc = " Concurrency: If-Match works like on PUT /packs/{id}; without it the update fails with 409 only if the pack changed while the request was processed."

// createdItemOutput is the full pack plus the id of the new element.
type createdItemOutput struct {
	CreatedID string `header:"X-Created-Id" doc:"ID of the created element"`
	ETag      string `header:"ETag" doc:"Quoted pack version; send it back as If-Match"`
	Body      *packs.Pack
}

type reorderBody struct {
	IDs []string `json:"ids" minItems:"1" doc:"Every existing id exactly once, in the new order"`
}

type addRoundInput struct {
	ID      string      `path:"id" doc:"Pack ID"`
	IfMatch string      `header:"If-Match" doc:"Optional optimistic-concurrency guard: the quoted pack version as returned in ETag, e.g. \"3\". When present it must equal the stored version (409 otherwise); a malformed value yields 412."`
	At      optionalInt `query:"at" minimum:"0" doc:"Insert position (0-based); omitted = append"`
	Body    packs.Round `doc:"The round to add; its id is ignored and generated"`
}

type roundInput struct {
	ID      string      `path:"id" doc:"Pack ID"`
	RoundID string      `path:"roundId" doc:"Round ID"`
	IfMatch string      `header:"If-Match" doc:"Optional optimistic-concurrency guard: the quoted pack version as returned in ETag, e.g. \"3\". When present it must equal the stored version (409 otherwise); a malformed value yields 412."`
	Body    packs.Round `doc:"The full replacement round including its themes; the id in the path wins"`
}

type roundRefInput struct {
	ID      string `path:"id" doc:"Pack ID"`
	RoundID string `path:"roundId" doc:"Round ID"`
	IfMatch string `header:"If-Match" doc:"Optional optimistic-concurrency guard: the quoted pack version as returned in ETag, e.g. \"3\". When present it must equal the stored version (409 otherwise); a malformed value yields 412."`
}

type reorderRoundsInput struct {
	ID      string      `path:"id" doc:"Pack ID"`
	IfMatch string      `header:"If-Match" doc:"Optional optimistic-concurrency guard: the quoted pack version as returned in ETag, e.g. \"3\". When present it must equal the stored version (409 otherwise); a malformed value yields 412."`
	Body    reorderBody `doc:"The round ids in their new order"`
}

type addThemeInput struct {
	ID      string      `path:"id" doc:"Pack ID"`
	RoundID string      `path:"roundId" doc:"Round ID"`
	IfMatch string      `header:"If-Match" doc:"Optional optimistic-concurrency guard: the quoted pack version as returned in ETag, e.g. \"3\". When present it must equal the stored version (409 otherwise); a malformed value yields 412."`
	At      optionalInt `query:"at" minimum:"0" doc:"Insert position (0-based); omitted = append"`
	Body    packs.Theme `doc:"The theme to add; its id is ignored and generated"`
}

type themeInput struct {
	ID      string      `path:"id" doc:"Pack ID"`
	RoundID string      `path:"roundId" doc:"Round ID"`
	ThemeID string      `path:"themeId" doc:"Theme ID"`
	IfMatch string      `header:"If-Match" doc:"Optional optimistic-concurrency guard: the quoted pack version as returned in ETag, e.g. \"3\". When present it must equal the stored version (409 otherwise); a malformed value yields 412."`
	Body    packs.Theme `doc:"The full replacement theme including its questions; the id in the path wins"`
}

type themeRefInput struct {
	ID      string `path:"id" doc:"Pack ID"`
	RoundID string `path:"roundId" doc:"Round ID"`
	ThemeID string `path:"themeId" doc:"Theme ID"`
	IfMatch string `header:"If-Match" doc:"Optional optimistic-concurrency guard: the quoted pack version as returned in ETag, e.g. \"3\". When present it must equal the stored version (409 otherwise); a malformed value yields 412."`
}

type reorderThemesInput struct {
	ID      string      `path:"id" doc:"Pack ID"`
	RoundID string      `path:"roundId" doc:"Round ID"`
	IfMatch string      `header:"If-Match" doc:"Optional optimistic-concurrency guard: the quoted pack version as returned in ETag, e.g. \"3\". When present it must equal the stored version (409 otherwise); a malformed value yields 412."`
	Body    reorderBody `doc:"The theme ids in their new order"`
}

type addQuestionInput struct {
	ID      string         `path:"id" doc:"Pack ID"`
	RoundID string         `path:"roundId" doc:"Round ID"`
	ThemeID string         `path:"themeId" doc:"Theme ID"`
	IfMatch string         `header:"If-Match" doc:"Optional optimistic-concurrency guard: the quoted pack version as returned in ETag, e.g. \"3\". When present it must equal the stored version (409 otherwise); a malformed value yields 412."`
	At      optionalInt    `query:"at" minimum:"0" doc:"Insert position (0-based); omitted = append"`
	Body    packs.Question `doc:"The question to add; its id is ignored and generated"`
}

type questionInput struct {
	ID         string         `path:"id" doc:"Pack ID"`
	RoundID    string         `path:"roundId" doc:"Round ID"`
	ThemeID    string         `path:"themeId" doc:"Theme ID"`
	QuestionID string         `path:"questionId" doc:"Question ID"`
	IfMatch    string         `header:"If-Match" doc:"Optional optimistic-concurrency guard: the quoted pack version as returned in ETag, e.g. \"3\". When present it must equal the stored version (409 otherwise); a malformed value yields 412."`
	Body       packs.Question `doc:"The full replacement question; the id in the path wins"`
}

type questionRefInput struct {
	ID         string `path:"id" doc:"Pack ID"`
	RoundID    string `path:"roundId" doc:"Round ID"`
	ThemeID    string `path:"themeId" doc:"Theme ID"`
	QuestionID string `path:"questionId" doc:"Question ID"`
	IfMatch    string `header:"If-Match" doc:"Optional optimistic-concurrency guard: the quoted pack version as returned in ETag, e.g. \"3\". When present it must equal the stored version (409 otherwise); a malformed value yields 412."`
}

type reorderQuestionsInput struct {
	ID      string      `path:"id" doc:"Pack ID"`
	RoundID string      `path:"roundId" doc:"Round ID"`
	ThemeID string      `path:"themeId" doc:"Theme ID"`
	IfMatch string      `header:"If-Match" doc:"Optional optimistic-concurrency guard: the quoted pack version as returned in ETag, e.g. \"3\". When present it must equal the stored version (409 otherwise); a malformed value yields 412."`
	Body    reorderBody `doc:"The question ids in their new order"`
}

func (s *Server) registerNested() {
	const (
		roundsPath    = basePath + "/packs/{id}/rounds"
		themesPath    = roundsPath + "/{roundId}/themes"
		questionsPath = themesPath + "/{themeId}/questions"
	)
	mutErrs := []int{http.StatusNotFound, http.StatusConflict, http.StatusPreconditionFailed, http.StatusUnprocessableEntity}
	bodyOp := func(op huma.Operation) huma.Operation {
		op.SkipValidateBody = true
		op.MaxBodyBytes = maxJSONBodyBytes
		op.BodyReadTimeout = bodyReadTimeout
		op.Errors = mutErrs
		return op
	}

	// Rounds.
	huma.Register(s.API, bodyOp(huma.Operation{
		OperationID: "addRound", Method: http.MethodPost, Path: roundsPath, Tags: []string{tagPacks},
		Summary:       "Add a round",
		Description:   "Inserts a round (with any themes and questions it carries) at position `at` or at the end, then validates and stores the pack. Returns the full pack; the new round id is in X-Created-Id." + nestedConcDesc,
		DefaultStatus: http.StatusCreated,
	}), s.addRound)
	huma.Register(s.API, bodyOp(huma.Operation{
		OperationID: "updateRound", Method: http.MethodPut, Path: roundsPath + "/{roundId}", Tags: []string{tagPacks},
		Summary:     "Replace a round",
		Description: "Replaces the whole round, including its themes and questions (send them back to keep them). Theme/question ids that are omitted are regenerated." + nestedConcDesc,
	}), s.updateRound)
	huma.Register(s.API, huma.Operation{
		OperationID: "deleteRound", Method: http.MethodDelete, Path: roundsPath + "/{roundId}", Tags: []string{tagPacks},
		Summary:     "Delete a round",
		Description: "Removes the round with all its themes and questions and returns the updated pack." + nestedConcDesc,
		Errors:      mutErrs,
	}, s.deleteRound)
	huma.Register(s.API, huma.Operation{
		OperationID: "reorderRounds", Method: http.MethodPost, Path: roundsPath + "/reorder", Tags: []string{tagPacks},
		Summary:     "Reorder rounds",
		Description: "Sets the round order. `ids` must contain every existing round id exactly once." + nestedConcDesc,
		Errors:      mutErrs,
	}, s.reorderRounds)

	// Themes.
	huma.Register(s.API, bodyOp(huma.Operation{
		OperationID: "addTheme", Method: http.MethodPost, Path: themesPath, Tags: []string{tagPacks},
		Summary:       "Add a theme",
		Description:   "Inserts a theme (with any questions it carries) into the round at position `at` or at the end. Returns the full pack; the new theme id is in X-Created-Id." + nestedConcDesc,
		DefaultStatus: http.StatusCreated,
	}), s.addTheme)
	huma.Register(s.API, bodyOp(huma.Operation{
		OperationID: "updateTheme", Method: http.MethodPut, Path: themesPath + "/{themeId}", Tags: []string{tagPacks},
		Summary:     "Replace a theme",
		Description: "Replaces the whole theme including its questions (send them back to keep them)." + nestedConcDesc,
	}), s.updateTheme)
	huma.Register(s.API, huma.Operation{
		OperationID: "deleteTheme", Method: http.MethodDelete, Path: themesPath + "/{themeId}", Tags: []string{tagPacks},
		Summary:     "Delete a theme",
		Description: "Removes the theme with all its questions and returns the updated pack." + nestedConcDesc,
		Errors:      mutErrs,
	}, s.deleteTheme)
	huma.Register(s.API, huma.Operation{
		OperationID: "reorderThemes", Method: http.MethodPost, Path: themesPath + "/reorder", Tags: []string{tagPacks},
		Summary:     "Reorder themes",
		Description: "Sets the theme order inside the round. `ids` must contain every existing theme id exactly once." + nestedConcDesc,
		Errors:      mutErrs,
	}, s.reorderThemes)

	// Questions.
	huma.Register(s.API, bodyOp(huma.Operation{
		OperationID: "addQuestion", Method: http.MethodPost, Path: questionsPath, Tags: []string{tagPacks},
		Summary:       "Add a question",
		Description:   "Inserts a question into the theme at position `at` or at the end. Returns the full pack; the new question id is in X-Created-Id." + nestedConcDesc,
		DefaultStatus: http.StatusCreated,
	}), s.addQuestion)
	huma.Register(s.API, bodyOp(huma.Operation{
		OperationID: "updateQuestion", Method: http.MethodPut, Path: questionsPath + "/{questionId}", Tags: []string{tagPacks},
		Summary:     "Replace a question",
		Description: "Replaces the whole question." + nestedConcDesc,
	}), s.updateQuestion)
	huma.Register(s.API, huma.Operation{
		OperationID: "deleteQuestion", Method: http.MethodDelete, Path: questionsPath + "/{questionId}", Tags: []string{tagPacks},
		Summary:     "Delete a question",
		Description: "Removes the question and returns the updated pack." + nestedConcDesc,
		Errors:      mutErrs,
	}, s.deleteQuestion)
	huma.Register(s.API, huma.Operation{
		OperationID: "reorderQuestions", Method: http.MethodPost, Path: questionsPath + "/reorder", Tags: []string{tagPacks},
		Summary:     "Reorder questions",
		Description: "Sets the question order inside the theme. `ids` must contain every existing question id exactly once." + nestedConcDesc,
		Errors:      mutErrs,
	}, s.reorderQuestions)
}

// --- generic mutation --------------------------------------------------------

// mutatePack loads the pack, applies fn and stores the result with the
// version read at load time (or the If-Match version) as the expected one.
func (s *Server) mutatePack(ctx context.Context, id, ifMatch string, fn func(p *packs.Pack) error) (*packs.Pack, error) {
	expected, err := parseIfMatch(ifMatch)
	if err != nil {
		return nil, err
	}
	p, err := s.deps.Packs.Get(ctx, id)
	if err != nil {
		return nil, s.fail(err)
	}
	if expected == 0 {
		expected = p.Version
	}
	if err := fn(p); err != nil {
		return nil, err
	}
	if err := s.deps.Packs.Update(ctx, p, expected); err != nil {
		if errors.Is(err, packs.ErrVersionConflict) {
			return nil, s.versionConflict(ctx, id)
		}
		return nil, s.fail(err)
	}
	return p, nil
}

func findRound(p *packs.Pack, id string) (int, error) {
	for i := range p.Rounds {
		if p.Rounds[i].ID == id {
			return i, nil
		}
	}
	return -1, huma.Error404NotFound("round not found in this pack")
}

func findTheme(r *packs.Round, id string) (int, error) {
	for i := range r.Themes {
		if r.Themes[i].ID == id {
			return i, nil
		}
	}
	return -1, huma.Error404NotFound("theme not found in this round")
}

func findQuestion(t *packs.Theme, id string) (int, error) {
	for i := range t.Questions {
		if t.Questions[i].ID == id {
			return i, nil
		}
	}
	return -1, huma.Error404NotFound("question not found in this theme")
}

// insertAt inserts item at position at (-1 = append); positions beyond the
// end are rejected with 422.
func insertAt[T any](list []T, at int, item T) ([]T, error) {
	if at == -1 || at == len(list) {
		return append(list, item), nil
	}
	if at < 0 || at > len(list) {
		return nil, huma.Error422UnprocessableEntity("insert position is out of range",
			&huma.ErrorDetail{Location: "query.at", Message: "must be between 0 and the current number of elements", Value: at})
	}
	out := make([]T, 0, len(list)+1)
	out = append(out, list[:at]...)
	out = append(out, item)
	out = append(out, list[at:]...)
	return out, nil
}

// reorderByID returns list in the order given by ids, which must be a
// permutation of the list's ids.
func reorderByID[T any](list []T, ids []string, idOf func(*T) string) ([]T, error) {
	byID := make(map[string]int, len(list))
	for i := range list {
		byID[idOf(&list[i])] = i
	}
	if len(ids) != len(list) {
		return nil, huma.Error422UnprocessableEntity("ids must list every element exactly once",
			&huma.ErrorDetail{Location: "body.ids", Message: "wrong number of ids", Value: len(ids)})
	}
	out := make([]T, 0, len(list))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		idx, ok := byID[id]
		if !ok {
			return nil, huma.Error422UnprocessableEntity("ids must list every element exactly once",
				&huma.ErrorDetail{Location: "body.ids", Message: "unknown id", Value: id})
		}
		if seen[id] {
			return nil, huma.Error422UnprocessableEntity("ids must list every element exactly once",
				&huma.ErrorDetail{Location: "body.ids", Message: "duplicate id", Value: id})
		}
		seen[id] = true
		out = append(out, list[idx])
	}
	return out, nil
}

func createdItem(p *packs.Pack, id string) *createdItemOutput {
	return &createdItemOutput{CreatedID: id, ETag: etag(p.Version), Body: p}
}

// --- rounds --------------------------------------------------------------------

func (s *Server) addRound(ctx context.Context, in *addRoundInput) (*createdItemOutput, error) {
	r := in.Body
	r.ID = packs.NewID()
	p, err := s.mutatePack(ctx, in.ID, in.IfMatch, func(p *packs.Pack) error {
		rounds, err := insertAt(p.Rounds, in.At.position(), r)
		if err != nil {
			return err
		}
		p.Rounds = rounds
		return nil
	})
	if err != nil {
		return nil, err
	}
	return createdItem(p, r.ID), nil
}

func (s *Server) updateRound(ctx context.Context, in *roundInput) (*packOutput, error) {
	p, err := s.mutatePack(ctx, in.ID, in.IfMatch, func(p *packs.Pack) error {
		i, err := findRound(p, in.RoundID)
		if err != nil {
			return err
		}
		r := in.Body
		r.ID = in.RoundID
		p.Rounds[i] = r
		return nil
	})
	if err != nil {
		return nil, err
	}
	return newPackOutput(p), nil
}

func (s *Server) deleteRound(ctx context.Context, in *roundRefInput) (*packOutput, error) {
	p, err := s.mutatePack(ctx, in.ID, in.IfMatch, func(p *packs.Pack) error {
		i, err := findRound(p, in.RoundID)
		if err != nil {
			return err
		}
		p.Rounds = append(p.Rounds[:i], p.Rounds[i+1:]...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return newPackOutput(p), nil
}

func (s *Server) reorderRounds(ctx context.Context, in *reorderRoundsInput) (*packOutput, error) {
	p, err := s.mutatePack(ctx, in.ID, in.IfMatch, func(p *packs.Pack) error {
		rounds, err := reorderByID(p.Rounds, in.Body.IDs, func(r *packs.Round) string { return r.ID })
		if err != nil {
			return err
		}
		p.Rounds = rounds
		return nil
	})
	if err != nil {
		return nil, err
	}
	return newPackOutput(p), nil
}

// --- themes --------------------------------------------------------------------

func (s *Server) addTheme(ctx context.Context, in *addThemeInput) (*createdItemOutput, error) {
	t := in.Body
	t.ID = packs.NewID()
	p, err := s.mutatePack(ctx, in.ID, in.IfMatch, func(p *packs.Pack) error {
		i, err := findRound(p, in.RoundID)
		if err != nil {
			return err
		}
		themes, err := insertAt(p.Rounds[i].Themes, in.At.position(), t)
		if err != nil {
			return err
		}
		p.Rounds[i].Themes = themes
		return nil
	})
	if err != nil {
		return nil, err
	}
	return createdItem(p, t.ID), nil
}

func (s *Server) updateTheme(ctx context.Context, in *themeInput) (*packOutput, error) {
	p, err := s.mutatePack(ctx, in.ID, in.IfMatch, func(p *packs.Pack) error {
		i, err := findRound(p, in.RoundID)
		if err != nil {
			return err
		}
		j, err := findTheme(&p.Rounds[i], in.ThemeID)
		if err != nil {
			return err
		}
		t := in.Body
		t.ID = in.ThemeID
		p.Rounds[i].Themes[j] = t
		return nil
	})
	if err != nil {
		return nil, err
	}
	return newPackOutput(p), nil
}

func (s *Server) deleteTheme(ctx context.Context, in *themeRefInput) (*packOutput, error) {
	p, err := s.mutatePack(ctx, in.ID, in.IfMatch, func(p *packs.Pack) error {
		i, err := findRound(p, in.RoundID)
		if err != nil {
			return err
		}
		j, err := findTheme(&p.Rounds[i], in.ThemeID)
		if err != nil {
			return err
		}
		r := &p.Rounds[i]
		r.Themes = append(r.Themes[:j], r.Themes[j+1:]...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return newPackOutput(p), nil
}

func (s *Server) reorderThemes(ctx context.Context, in *reorderThemesInput) (*packOutput, error) {
	p, err := s.mutatePack(ctx, in.ID, in.IfMatch, func(p *packs.Pack) error {
		i, err := findRound(p, in.RoundID)
		if err != nil {
			return err
		}
		themes, err := reorderByID(p.Rounds[i].Themes, in.Body.IDs, func(t *packs.Theme) string { return t.ID })
		if err != nil {
			return err
		}
		p.Rounds[i].Themes = themes
		return nil
	})
	if err != nil {
		return nil, err
	}
	return newPackOutput(p), nil
}

// --- questions -----------------------------------------------------------------

func (s *Server) addQuestion(ctx context.Context, in *addQuestionInput) (*createdItemOutput, error) {
	q := in.Body
	q.ID = packs.NewID()
	p, err := s.mutatePack(ctx, in.ID, in.IfMatch, func(p *packs.Pack) error {
		i, err := findRound(p, in.RoundID)
		if err != nil {
			return err
		}
		j, err := findTheme(&p.Rounds[i], in.ThemeID)
		if err != nil {
			return err
		}
		questions, err := insertAt(p.Rounds[i].Themes[j].Questions, in.At.position(), q)
		if err != nil {
			return err
		}
		p.Rounds[i].Themes[j].Questions = questions
		return nil
	})
	if err != nil {
		return nil, err
	}
	return createdItem(p, q.ID), nil
}

func (s *Server) updateQuestion(ctx context.Context, in *questionInput) (*packOutput, error) {
	p, err := s.mutatePack(ctx, in.ID, in.IfMatch, func(p *packs.Pack) error {
		i, err := findRound(p, in.RoundID)
		if err != nil {
			return err
		}
		j, err := findTheme(&p.Rounds[i], in.ThemeID)
		if err != nil {
			return err
		}
		k, err := findQuestion(&p.Rounds[i].Themes[j], in.QuestionID)
		if err != nil {
			return err
		}
		q := in.Body
		q.ID = in.QuestionID
		p.Rounds[i].Themes[j].Questions[k] = q
		return nil
	})
	if err != nil {
		return nil, err
	}
	return newPackOutput(p), nil
}

func (s *Server) deleteQuestion(ctx context.Context, in *questionRefInput) (*packOutput, error) {
	p, err := s.mutatePack(ctx, in.ID, in.IfMatch, func(p *packs.Pack) error {
		i, err := findRound(p, in.RoundID)
		if err != nil {
			return err
		}
		j, err := findTheme(&p.Rounds[i], in.ThemeID)
		if err != nil {
			return err
		}
		t := &p.Rounds[i].Themes[j]
		k, err := findQuestion(t, in.QuestionID)
		if err != nil {
			return err
		}
		t.Questions = append(t.Questions[:k], t.Questions[k+1:]...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return newPackOutput(p), nil
}

func (s *Server) reorderQuestions(ctx context.Context, in *reorderQuestionsInput) (*packOutput, error) {
	p, err := s.mutatePack(ctx, in.ID, in.IfMatch, func(p *packs.Pack) error {
		i, err := findRound(p, in.RoundID)
		if err != nil {
			return err
		}
		j, err := findTheme(&p.Rounds[i], in.ThemeID)
		if err != nil {
			return err
		}
		questions, err := reorderByID(p.Rounds[i].Themes[j].Questions, in.Body.IDs, func(q *packs.Question) string { return q.ID })
		if err != nil {
			return err
		}
		p.Rounds[i].Themes[j].Questions = questions
		return nil
	})
	if err != nil {
		return nil, err
	}
	return newPackOutput(p), nil
}
