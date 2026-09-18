package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"sigame/internal/packs"
)

func TestPacksCRUDAndConcurrency(t *testing.T) {
	e := newTestEnv(t)

	// Minimal body: only the name.
	rec := e.do(t, http.MethodPost, "/api/v1/packs", map[string]any{"name": "Тестовый пак"})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, `"1"`, rec.Header().Get("ETag"))
	var created packs.Pack
	decodeJSON(t, rec, &created)
	require.NotEmpty(t, created.ID)
	require.Equal(t, "/api/v1/packs/"+created.ID, rec.Header().Get("Location"))
	require.Equal(t, 1, created.Version)
	require.Equal(t, packs.DefaultDifficulty, created.Difficulty)
	require.NotZero(t, created.CreatedAt)

	// Get with ETag.
	rec = e.do(t, http.MethodGet, "/api/v1/packs/"+created.ID, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, `"1"`, rec.Header().Get("ETag"))
	var got packs.Pack
	decodeJSON(t, rec, &got)
	require.Equal(t, "Тестовый пак", got.Name)

	// Update with a matching If-Match.
	got.Name = "Renamed"
	rec = e.do(t, http.MethodPut, "/api/v1/packs/"+created.ID, got, "If-Match", `"1"`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, `"2"`, rec.Header().Get("ETag"))
	var updated packs.Pack
	decodeJSON(t, rec, &updated)
	require.Equal(t, 2, updated.Version)
	require.Equal(t, "Renamed", updated.Name)
	require.Equal(t, created.CreatedAt, updated.CreatedAt)

	// Stale If-Match → 409 with the current version.
	rec = e.do(t, http.MethodPut, "/api/v1/packs/"+created.ID, got, "If-Match", `"1"`)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	p := decodeProblem(t, rec)
	require.Contains(t, p.Detail, "2")
	require.Len(t, p.Errors, 1)
	require.Equal(t, "version", p.Errors[0].Location)
	require.EqualValues(t, 2, p.Errors[0].Value)

	// Stale body.version without If-Match → 409 as well.
	got.Version = 1
	rec = e.do(t, http.MethodPut, "/api/v1/packs/"+created.ID, got)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	// Malformed If-Match → 412.
	rec = e.do(t, http.MethodPut, "/api/v1/packs/"+created.ID, got, "If-Match", "abc")
	require.Equal(t, http.StatusPreconditionFailed, rec.Code, rec.Body.String())

	// Unconditional update (no If-Match, version 0) succeeds.
	got.Version = 0
	got.Name = "Renamed again"
	rec = e.do(t, http.MethodPut, "/api/v1/packs/"+created.ID, got)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, `"3"`, rec.Header().Get("ETag"))

	// Unknown pack → 404 problem.
	rec = e.do(t, http.MethodGet, "/api/v1/packs/0190a000-0000-7000-8000-000000000000", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, http.StatusNotFound, decodeProblem(t, rec).Status)

	// Delete → 204, then 404.
	rec = e.do(t, http.MethodDelete, "/api/v1/packs/"+created.ID, nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	rec = e.do(t, http.MethodGet, "/api/v1/packs/"+created.ID, nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = e.do(t, http.MethodDelete, "/api/v1/packs/"+created.ID, nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPacksValidationErrors(t *testing.T) {
	e := newTestEnv(t)

	rec := e.do(t, http.MethodPost, "/api/v1/packs", map[string]any{
		"name":       "   ",
		"difficulty": 42,
		"rounds": []map[string]any{{
			"name": "R", "type": "weird",
			"themes": []map[string]any{{"name": "T", "questions": []map[string]any{{"price": 100, "params": map[string]any{"question": []any{}}}}}},
		}},
	})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := decodeProblem(t, rec)
	locations := map[string]string{}
	for _, d := range p.Errors {
		locations[d.Location] = d.Value.(string)
	}
	require.Equal(t, packs.CodeRequired, locations["/name"])
	require.Equal(t, packs.CodeOutOfRange, locations["/difficulty"])
	require.Equal(t, packs.CodeInvalidValue, locations["/rounds/0/type"])
	require.Equal(t, packs.CodeTooFew, locations["/rounds/0/themes/0/questions/0/params/question"])

	// Malformed JSON → 422 (huma reports it as a body validation failure).
	rec = e.do(t, http.MethodPost, "/api/v1/packs", "{not json")
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Equal(t, "body", decodeProblem(t, rec).Errors[0].Location)

	// Reference to unknown media → 422 mediaNotFound with the item's pointer.
	pk := samplePack("Media pack")
	pk.Rounds[0].Themes[0].Questions[0].Params.Question = []packs.ContentItem{{
		Type: packs.ContentImage, MediaID: strings.Repeat("ab", 32),
	}}
	rec = e.do(t, http.MethodPost, "/api/v1/packs", pk)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p = decodeProblem(t, rec)
	require.Len(t, p.Errors, 1)
	require.Equal(t, "/rounds/0/themes/0/questions/0/params/question/0/mediaId", p.Errors[0].Location)
	require.Equal(t, packs.CodeMediaNotFound, p.Errors[0].Value)

	// The validate endpoint reports the same problems with 200.
	rec = e.do(t, http.MethodPost, "/api/v1/packs/validate", pk)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var vr ValidationResult
	decodeJSON(t, rec, &vr)
	require.False(t, vr.Valid)
	require.Len(t, vr.Problems, 1)
	require.Equal(t, packs.CodeMediaNotFound, vr.Problems[0].Code)
	require.Equal(t, "/rounds/0/themes/0/questions/0/params/question/0/mediaId", vr.Problems[0].Path)

	rec = e.do(t, http.MethodPost, "/api/v1/packs/validate", map[string]any{"name": "", "difficulty": 99})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	decodeJSON(t, rec, &vr)
	require.False(t, vr.Valid)
	require.Len(t, vr.Problems, 2)

	rec = e.do(t, http.MethodPost, "/api/v1/packs/validate", samplePack("ok"))
	require.Equal(t, http.StatusOK, rec.Code)
	decodeJSON(t, rec, &vr)
	require.True(t, vr.Valid)
	require.Empty(t, vr.Problems)
}

func TestPacksListAndDuplicate(t *testing.T) {
	e := newTestEnv(t)
	e.createPack(t, samplePack("Столицы мира"))
	e.createPack(t, samplePack("История России"))
	e.createPack(t, samplePack("Science"))

	rec := e.do(t, http.MethodGet, "/api/v1/packs?q=%D1%81%D1%82%D0%BE%D0%BB%D0%B8%D1%86%D1%8B", nil) // "столицы"
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var list PackList
	decodeJSON(t, rec, &list)
	require.Equal(t, 1, list.Total)
	require.Len(t, list.Items, 1)
	require.Equal(t, "Столицы мира", list.Items[0].Name)

	rec = e.do(t, http.MethodGet, "/api/v1/packs?limit=2&offset=1&sort=name&order=asc", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	decodeJSON(t, rec, &list)
	require.Equal(t, 3, list.Total)
	require.Len(t, list.Items, 2)
	require.Equal(t, 2, list.Limit)
	require.Equal(t, 1, list.Offset)
	// Byte order of the lower-cased names: "science" < "история россии" < "столицы мира".
	require.Equal(t, "История России", list.Items[0].Name)
	require.Equal(t, "Столицы мира", list.Items[1].Name)

	rec = e.do(t, http.MethodGet, "/api/v1/packs?tag=test&hasMedia=false", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	decodeJSON(t, rec, &list)
	require.Equal(t, 3, list.Total)
	rec = e.do(t, http.MethodGet, "/api/v1/packs?hasMedia=true", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	decodeJSON(t, rec, &list)
	require.Equal(t, 0, list.Total)

	// Query validation is huma's job.
	rec = e.do(t, http.MethodGet, "/api/v1/packs?limit=500", nil)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	rec = e.do(t, http.MethodGet, "/api/v1/packs?sort=price", nil)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	// Duplicate.
	rec = e.do(t, http.MethodGet, "/api/v1/packs?q=Science", nil)
	decodeJSON(t, rec, &list)
	id := list.Items[0].ID
	rec = e.do(t, http.MethodPost, "/api/v1/packs/"+id+"/duplicate", nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var cp packs.Pack
	decodeJSON(t, rec, &cp)
	require.NotEqual(t, id, cp.ID)
	require.Equal(t, "Science (copy)", cp.Name)
	require.Equal(t, 1, cp.Version)

	rec = e.do(t, http.MethodPost, "/api/v1/packs/"+id+"/duplicate", map[string]any{"name": "Science 2"})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	decodeJSON(t, rec, &cp)
	require.Equal(t, "Science 2", cp.Name)

	rec = e.do(t, http.MethodPost, "/api/v1/packs/0190a000-0000-7000-8000-000000000000/duplicate", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPacksNestedEditing(t *testing.T) {
	e := newTestEnv(t)
	p := e.createPack(t, samplePack("Nested"))
	base := "/api/v1/packs/" + p.ID

	// Add a round at the front.
	rec := e.do(t, http.MethodPost, base+"/rounds?at=0", packs.Round{Name: "Round 0", Type: packs.RoundStandard})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	roundID := rec.Header().Get("X-Created-Id")
	require.NotEmpty(t, roundID)
	require.Equal(t, `"2"`, rec.Header().Get("ETag"))
	decodeJSON(t, rec, &p)
	require.Len(t, p.Rounds, 2)
	require.Equal(t, roundID, p.Rounds[0].ID)
	require.Equal(t, "Round 0", p.Rounds[0].Name)
	require.Equal(t, 2, p.Version)

	// Out-of-range insert → 422.
	rec = e.do(t, http.MethodPost, base+"/rounds?at=7", packs.Round{Name: "x"})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Equal(t, "query.at", decodeProblem(t, rec).Errors[0].Location)
	rec = e.do(t, http.MethodPost, base+"/rounds?at=-1", packs.Round{Name: "x"})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	rec = e.do(t, http.MethodPost, base+"/rounds?at=x", packs.Round{Name: "x"})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())

	// Add a theme and a question into the new round.
	rec = e.do(t, http.MethodPost, base+"/rounds/"+roundID+"/themes", packs.Theme{Name: "Theme A"})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	themeID := rec.Header().Get("X-Created-Id")
	decodeJSON(t, rec, &p)
	require.Equal(t, themeID, p.Rounds[0].Themes[0].ID)

	q := packs.Question{Price: 200, Params: packs.QuestionParams{Question: []packs.ContentItem{{Type: packs.ContentText, Text: "Q1"}}}, Right: []string{"A1"}}
	rec = e.do(t, http.MethodPost, base+"/rounds/"+roundID+"/themes/"+themeID+"/questions", q)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	q1 := rec.Header().Get("X-Created-Id")
	q.Price = 300
	q.Params.Question[0].Text = "Q2"
	rec = e.do(t, http.MethodPost, base+"/rounds/"+roundID+"/themes/"+themeID+"/questions", q)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	q2 := rec.Header().Get("X-Created-Id")
	decodeJSON(t, rec, &p)
	require.Equal(t, []string{q1, q2}, []string{p.Rounds[0].Themes[0].Questions[0].ID, p.Rounds[0].Themes[0].Questions[1].ID})
	version := p.Version

	// Validation of nested bodies uses pack-level JSON pointers.
	rec = e.do(t, http.MethodPost, base+"/rounds/"+roundID+"/themes/"+themeID+"/questions", packs.Question{Price: -5})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Equal(t, "/rounds/0/themes/0/questions/2/price", decodeProblem(t, rec).Errors[0].Location)

	// Reorder questions.
	rec = e.do(t, http.MethodPost, base+"/rounds/"+roundID+"/themes/"+themeID+"/questions/reorder", map[string]any{"ids": []string{q2, q1}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	decodeJSON(t, rec, &p)
	require.Equal(t, q2, p.Rounds[0].Themes[0].Questions[0].ID)
	require.Equal(t, version+1, p.Version)

	rec = e.do(t, http.MethodPost, base+"/rounds/"+roundID+"/themes/"+themeID+"/questions/reorder", map[string]any{"ids": []string{q2, q2}})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Equal(t, "body.ids", decodeProblem(t, rec).Errors[0].Location)
	rec = e.do(t, http.MethodPost, base+"/rounds/"+roundID+"/themes/"+themeID+"/questions/reorder", map[string]any{"ids": []string{q2}})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())

	// Update a question (id from the path wins) and a theme.
	q.Price = 500
	q.ID = "ignored"
	rec = e.do(t, http.MethodPut, base+"/rounds/"+roundID+"/themes/"+themeID+"/questions/"+q1, q)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	decodeJSON(t, rec, &p)
	require.Equal(t, q1, p.Rounds[0].Themes[0].Questions[1].ID)
	require.Equal(t, 500, p.Rounds[0].Themes[0].Questions[1].Price)

	theme := p.Rounds[0].Themes[0]
	theme.Name = "Theme A renamed"
	rec = e.do(t, http.MethodPut, base+"/rounds/"+roundID+"/themes/"+themeID, theme)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	decodeJSON(t, rec, &p)
	require.Equal(t, "Theme A renamed", p.Rounds[0].Themes[0].Name)
	require.Len(t, p.Rounds[0].Themes[0].Questions, 2)

	// Reorder rounds, then delete question/theme/round.
	rec = e.do(t, http.MethodPost, base+"/rounds/reorder", map[string]any{"ids": []string{p.Rounds[1].ID, p.Rounds[0].ID}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	decodeJSON(t, rec, &p)
	require.Equal(t, roundID, p.Rounds[1].ID)

	rec = e.do(t, http.MethodDelete, base+"/rounds/"+roundID+"/themes/"+themeID+"/questions/"+q2, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	decodeJSON(t, rec, &p)
	require.Len(t, p.Rounds[1].Themes[0].Questions, 1)

	rec = e.do(t, http.MethodDelete, base+"/rounds/"+roundID+"/themes/"+themeID, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	decodeJSON(t, rec, &p)
	require.Empty(t, p.Rounds[1].Themes)

	// Unknown ids → 404.
	rec = e.do(t, http.MethodDelete, base+"/rounds/"+roundID+"/themes/"+themeID, nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = e.do(t, http.MethodDelete, base+"/rounds/nope", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)

	// If-Match on nested operations.
	rec = e.do(t, http.MethodDelete, base+"/rounds/"+roundID, nil, "If-Match", `"1"`)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	rec = e.do(t, http.MethodDelete, base+"/rounds/"+roundID, nil, "If-Match", etag(p.Version))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	decodeJSON(t, rec, &p)
	require.Len(t, p.Rounds, 1)
	require.Equal(t, "Round 1", p.Rounds[0].Name)
}
