package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"sigame/internal/media"
	"sigame/internal/packs"
)

// --- shared DTO pieces -----------------------------------------------------

// packIDInput identifies a pack.
type packIDInput struct {
	ID string `path:"id" doc:"Pack ID (UUIDv7)"`
}

// ifMatchDoc documents the optimistic-concurrency header.
const ifMatchDoc = "Optional optimistic-concurrency guard: the quoted pack version as returned in ETag, e.g. \"3\". " +
	"When present it must equal the stored version (409 otherwise); a malformed value yields 412."

// packOutput returns the full pack with its version as ETag.
type packOutput struct {
	ETag string `header:"ETag" doc:"Quoted pack version; send it back as If-Match"`
	Body *packs.Pack
}

// createdPackOutput is packOutput plus the Location of the new pack.
type createdPackOutput struct {
	Location string `header:"Location" doc:"URL of the created pack"`
	ETag     string `header:"ETag" doc:"Quoted pack version; send it back as If-Match"`
	Body     *packs.Pack
}

// PackList is one page of pack summaries.
type PackList struct {
	Items  []packs.Summary `json:"items" doc:"Summaries of the matching packs, in the requested order"`
	Total  int             `json:"total" doc:"Total number of packs matching the filter, ignoring limit/offset"`
	Limit  int             `json:"limit" doc:"Effective page size"`
	Offset int             `json:"offset" doc:"Effective offset"`
}

// ValidationResult is the answer of POST /packs/validate.
type ValidationResult struct {
	Valid    bool            `json:"valid" doc:"True when no problem was found"`
	Problems []packs.Problem `json:"problems" doc:"Every problem found; empty when valid"`
}

func etag(version int) string { return `"` + strconv.Itoa(version) + `"` }

func packLocation(id string) string { return basePath + "/packs/" + id }

func newPackOutput(p *packs.Pack) *packOutput {
	return &packOutput{ETag: etag(p.Version), Body: p}
}

func newCreatedPackOutput(p *packs.Pack) *createdPackOutput {
	return &createdPackOutput{Location: packLocation(p.ID), ETag: etag(p.Version), Body: p}
}

// parseIfMatch returns the version carried by an If-Match header, 0 when the
// header is absent or "*", and a 412 error when it is malformed.
func parseIfMatch(h string) (int, error) {
	h = strings.TrimSpace(h)
	if h == "" || h == "*" {
		return 0, nil
	}
	h = strings.TrimPrefix(h, "W/")
	if len(h) >= 2 && h[0] == '"' && h[len(h)-1] == '"' {
		h = h[1 : len(h)-1]
	}
	n, err := strconv.Atoi(h)
	if err != nil || n < 1 {
		return 0, huma.Error412PreconditionFailed(`If-Match must be a quoted pack version, e.g. "3"`,
			&huma.ErrorDetail{Location: "header.If-Match", Message: "malformed pack version", Value: strings.TrimSpace(h)})
	}
	return n, nil
}

// expectedVersion combines If-Match (takes precedence) with a body version.
func expectedVersion(ifMatch string, bodyVersion int) (int, error) {
	v, err := parseIfMatch(ifMatch)
	if err != nil {
		return 0, err
	}
	if v == 0 && bodyVersion > 0 {
		v = bodyVersion
	}
	return v, nil
}

// --- operations --------------------------------------------------------------

type listPacksInput struct {
	Q             string `query:"q" maxLength:"200" doc:"Case-insensitive substring of the name, authors or tags (Unicode-aware)"`
	Language      string `query:"language" maxLength:"16" doc:"Exact BCP-47 language tag, e.g. ru-RU"`
	Tag           string `query:"tag" maxLength:"350" doc:"Case-insensitive exact match of one tag"`
	MinDifficulty int    `query:"minDifficulty" minimum:"0" maximum:"10" doc:"Inclusive lower bound; 0 = unbounded"`
	MaxDifficulty int    `query:"maxDifficulty" minimum:"0" maximum:"10" doc:"Inclusive upper bound; 0 = unbounded"`
	HasMedia      string `query:"hasMedia" enum:"true,false" doc:"Only packs with (true) or without (false) media items"`
	Sort          string `query:"sort" enum:"updatedAt,name,createdAt" default:"updatedAt" doc:"Sort key"`
	Order         string `query:"order" enum:"asc,desc" doc:"Sort direction; default desc for timestamps, asc for name"`
	Limit         int    `query:"limit" minimum:"1" maximum:"200" default:"50" doc:"Page size (1..200)"`
	Offset        int    `query:"offset" minimum:"0" doc:"Number of packs to skip"`
}

type listPacksOutput struct {
	Body PackList
}

type createPackInput struct {
	Body packs.Pack `doc:"The pack to create. id, version, createdAt and updatedAt are ignored; a minimal body {\"name\": \"...\"} is enough. Rounds, themes and questions without an id receive one."`
}

type updatePackInput struct {
	ID      string     `path:"id" doc:"Pack ID (UUIDv7)"`
	IfMatch string     `header:"If-Match" doc:"Optional optimistic-concurrency guard: the quoted pack version as returned in ETag, e.g. \"3\". When present it must equal the stored version (409 otherwise); a malformed value yields 412. Without it body.version (when > 0) is used."`
	Body    packs.Pack `doc:"The full replacement pack. id in the path wins; createdAt is preserved; version is compared when If-Match is absent"`
}

type duplicatePackInput struct {
	ID   string `path:"id" doc:"Pack ID (UUIDv7)"`
	Body *struct {
		Name string `json:"name,omitempty" maxLength:"350" doc:"Name of the copy; default \"<name> (copy)\""`
	} `doc:"Optional new name"`
}

type validatePackInput struct {
	Body packs.Pack `doc:"The pack to check; it is normalised the same way as on save"`
}

type validatePackOutput struct {
	Body ValidationResult
}

func (s *Server) registerPacks() {
	huma.Register(s.API, huma.Operation{
		OperationID: "listPacks",
		Method:      http.MethodGet,
		Path:        basePath + "/packs",
		Tags:        []string{tagPacks},
		Summary:     "List packs",
		Description: "Returns one page of pack summaries (no rounds) matching the filter. Text search is case-insensitive and Unicode-aware, so Cyrillic names are found regardless of case.",
	}, s.listPacks)

	huma.Register(s.API, huma.Operation{
		OperationID:      "createPack",
		Method:           http.MethodPost,
		Path:             basePath + "/packs",
		Tags:             []string{tagPacks},
		Summary:          "Create a pack",
		Description:      "Stores a new pack. Server-managed fields (id, version, timestamps) in the body are ignored. The pack is normalised (trimmed strings, default round/question types, difficulty 5 when unset) and validated; every referenced mediaId must already be uploaded. Validation problems are returned as 422 with JSON-pointer locations.",
		DefaultStatus:    http.StatusCreated,
		Errors:           []int{http.StatusUnprocessableEntity},
		SkipValidateBody: true,
		MaxBodyBytes:     maxJSONBodyBytes,
		BodyReadTimeout:  bodyReadTimeout,
	}, s.createPack)

	huma.Register(s.API, huma.Operation{
		OperationID: "getPack",
		Method:      http.MethodGet,
		Path:        basePath + "/packs/{id}",
		Tags:        []string{tagPacks},
		Summary:     "Get a pack",
		Description: "Returns the full pack including rounds, themes and questions. The ETag header carries the quoted version for use in If-Match.",
		Errors:      []int{http.StatusNotFound},
	}, s.getPack)

	huma.Register(s.API, huma.Operation{
		OperationID:      "updatePack",
		Method:           http.MethodPut,
		Path:             basePath + "/packs/{id}",
		Tags:             []string{tagPacks},
		Summary:          "Replace a pack",
		Description:      "Replaces the whole pack. Concurrency: If-Match (quoted version) takes precedence over body.version; when neither is given the update is unconditional. The version is incremented on success and returned as ETag.",
		Errors:           []int{http.StatusNotFound, http.StatusConflict, http.StatusPreconditionFailed, http.StatusUnprocessableEntity},
		SkipValidateBody: true,
		MaxBodyBytes:     maxJSONBodyBytes,
		BodyReadTimeout:  bodyReadTimeout,
	}, s.updatePack)

	huma.Register(s.API, huma.Operation{
		OperationID: "deletePack",
		Method:      http.MethodDelete,
		Path:        basePath + "/packs/{id}",
		Tags:        []string{tagPacks},
		Summary:     "Delete a pack",
		Description: "Deletes the pack and releases its media references; the media objects themselves stay in the store until they are deleted or garbage-collected.",
		Errors:      []int{http.StatusNotFound},
	}, s.deletePack)

	huma.Register(s.API, huma.Operation{
		OperationID:   "duplicatePack",
		Method:        http.MethodPost,
		Path:          basePath + "/packs/{id}/duplicate",
		Tags:          []string{tagPacks},
		Summary:       "Duplicate a pack",
		Description:   "Stores a deep copy of the pack with fresh ids for the pack, its rounds, themes and questions. Media is shared (reference-counted), the SIQ package id is cleared.",
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusNotFound},
	}, s.duplicatePack)

	huma.Register(s.API, huma.Operation{
		OperationID:      "validatePack",
		Method:           http.MethodPost,
		Path:             basePath + "/packs/validate",
		Tags:             []string{tagPacks},
		Summary:          "Validate a pack without saving",
		Description:      "Runs the same normalisation and validation as create/update, including the existence of referenced media, and always answers 200 with the list of problems (never 422).",
		SkipValidateBody: true,
		MaxBodyBytes:     maxJSONBodyBytes,
		BodyReadTimeout:  bodyReadTimeout,
	}, s.validatePack)
}

func (s *Server) listPacks(ctx context.Context, in *listPacksInput) (*listPacksOutput, error) {
	f := packs.ListFilter{
		Query:         in.Q,
		Language:      in.Language,
		Tag:           in.Tag,
		MinDifficulty: in.MinDifficulty,
		MaxDifficulty: in.MaxDifficulty,
		Sort:          in.Sort,
		Order:         in.Order,
		Limit:         in.Limit,
		Offset:        in.Offset,
	}
	switch in.HasMedia {
	case "true":
		t := true
		f.HasMedia = &t
	case "false":
		t := false
		f.HasMedia = &t
	}
	if f.Limit <= 0 {
		f.Limit = packs.DefaultListLimit
	}
	res, err := s.deps.Packs.List(ctx, f)
	if err != nil {
		return nil, s.fail(err)
	}
	return &listPacksOutput{Body: PackList{Items: res.Items, Total: res.Total, Limit: f.Limit, Offset: f.Offset}}, nil
}

func (s *Server) createPack(ctx context.Context, in *createPackInput) (*createdPackOutput, error) {
	p := in.Body
	p.ID = ""
	p.Version = 0
	p.CreatedAt = 0
	p.UpdatedAt = 0
	if err := s.deps.Packs.Create(ctx, &p); err != nil {
		return nil, s.fail(err)
	}
	return newCreatedPackOutput(&p), nil
}

func (s *Server) getPack(ctx context.Context, in *packIDInput) (*packOutput, error) {
	p, err := s.deps.Packs.Get(ctx, in.ID)
	if err != nil {
		return nil, s.fail(err)
	}
	return newPackOutput(p), nil
}

func (s *Server) updatePack(ctx context.Context, in *updatePackInput) (*packOutput, error) {
	expected, err := expectedVersion(in.IfMatch, in.Body.Version)
	if err != nil {
		return nil, err
	}
	p := in.Body
	p.ID = in.ID
	if err := s.deps.Packs.Update(ctx, &p, expected); err != nil {
		if errors.Is(err, packs.ErrVersionConflict) {
			return nil, s.versionConflict(ctx, in.ID)
		}
		return nil, s.fail(err)
	}
	return newPackOutput(&p), nil
}

func (s *Server) deletePack(ctx context.Context, in *packIDInput) (*struct{}, error) {
	if err := s.deps.Packs.Delete(ctx, in.ID); err != nil {
		return nil, s.fail(err)
	}
	return nil, nil
}

func (s *Server) duplicatePack(ctx context.Context, in *duplicatePackInput) (*createdPackOutput, error) {
	name := ""
	if in.Body != nil {
		name = in.Body.Name
	}
	p, err := s.deps.Packs.Duplicate(ctx, in.ID, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return newCreatedPackOutput(p), nil
}

func (s *Server) validatePack(ctx context.Context, in *validatePackInput) (*validatePackOutput, error) {
	p := in.Body
	packs.Normalize(&p)
	problems := []packs.Problem{}
	var ve *packs.ValidationError
	if err := packs.Validate(&p); errors.As(err, &ve) {
		problems = append(problems, ve.Problems...)
	} else if err != nil {
		return nil, s.fail(err)
	}
	// Media existence is otherwise checked by the repository on save.
	missing := map[string]bool{}
	for _, id := range p.MediaIDs() {
		_, err := s.deps.Media.Stat(ctx, id)
		switch {
		case errors.Is(err, media.ErrNotFound):
			missing[id] = true
		case err != nil:
			return nil, s.fail(err)
		}
	}
	if len(missing) > 0 {
		for _, ref := range mediaRefPaths(&p) {
			if missing[ref.id] {
				problems = append(problems, packs.Problem{
					Path:    ref.path,
					Code:    packs.CodeMediaNotFound,
					Message: fmt.Sprintf("media %s is not stored on this server", ref.id),
				})
			}
		}
	}
	return &validatePackOutput{Body: ValidationResult{Valid: len(problems) == 0, Problems: problems}}, nil
}

// mediaRef is one media reference with the JSON pointer of its location.
type mediaRef struct{ path, id string }

// mediaRefPaths walks the same places as packs.Pack.MediaIDs but keeps
// duplicates and records where each id is used.
func mediaRefPaths(p *packs.Pack) []mediaRef {
	var out []mediaRef
	items := func(path string, list []packs.ContentItem) {
		for i, it := range list {
			if it.MediaID != "" {
				out = append(out, mediaRef{fmt.Sprintf("%s/%d/mediaId", path, i), it.MediaID})
			}
		}
	}
	var params func(path string, list []packs.Param)
	params = func(path string, list []packs.Param) {
		for i, pr := range list {
			pp := fmt.Sprintf("%s/%d", path, i)
			items(pp+"/items", pr.Items)
			params(pp+"/params", pr.Params)
		}
	}
	if p.LogoMediaID != "" {
		out = append(out, mediaRef{"/logoMediaId", p.LogoMediaID})
	}
	for i, r := range p.Rounds {
		for j, t := range r.Themes {
			for k, q := range t.Questions {
				qp := fmt.Sprintf("/rounds/%d/themes/%d/questions/%d", i, j, k)
				items(qp+"/params/question", q.Params.Question)
				items(qp+"/params/answer", q.Params.Answer)
				for o, opt := range q.Params.AnswerOptions {
					items(fmt.Sprintf("%s/params/answerOptions/%d/content", qp, o), opt.Content)
				}
				params(qp+"/extraParams", q.Extra)
				for sIdx, step := range q.Script {
					params(fmt.Sprintf("%s/script/%d/params", qp, sIdx), step.Params)
				}
			}
		}
	}
	return out
}
