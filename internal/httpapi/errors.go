package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"sigame/internal/ai"
	"sigame/internal/media"
	"sigame/internal/packs"
	"sigame/internal/siq"
)

// mapError translates domain errors of packs, media, siq and ai into huma
// status errors (RFC 9457 problem documents). Unknown errors become a
// generic 500 whose detail does not leak internals. A nil err yields nil and
// an error that already is a huma.StatusError is returned unchanged.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	var se huma.StatusError
	if errors.As(err, &se) {
		return se
	}

	var ve *packs.ValidationError
	if errors.As(err, &ve) {
		details := make([]error, 0, len(ve.Problems))
		for _, p := range ve.Problems {
			details = append(details, &huma.ErrorDetail{Location: p.Path, Message: p.Message, Value: p.Code})
		}
		return huma.Error422UnprocessableEntity("pack validation failed", details...)
	}
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return huma.Error413RequestEntityTooLarge(fmt.Sprintf("request body exceeds %d bytes", tooBig.Limit))
	}

	switch {
	// packs
	case errors.Is(err, packs.ErrNotFound):
		return huma.Error404NotFound("pack not found")
	case errors.Is(err, packs.ErrVersionConflict):
		return huma.Error409Conflict("pack version conflict: the pack was modified by someone else; reload it and retry")
	case errors.Is(err, packs.ErrAlreadyExists):
		return huma.Error409Conflict("a pack with this id already exists")

	// media
	case errors.Is(err, media.ErrNotFound):
		return huma.Error404NotFound("media not found")
	case errors.Is(err, media.ErrTooLarge):
		return huma.Error413RequestEntityTooLarge(err.Error())
	case errors.Is(err, media.ErrUnsupported), errors.Is(err, media.ErrKindMismatch):
		return huma.Error415UnsupportedMediaType(err.Error())
	case errors.Is(err, media.ErrInUse):
		return huma.Error409Conflict(err.Error())

	// siq
	case errors.Is(err, siq.ErrBadZip), errors.Is(err, siq.ErrUnsafeEntry):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, siq.ErrNoContent), errors.Is(err, siq.ErrBadXML):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, siq.ErrTooManyEntries), errors.Is(err, siq.ErrTooLarge), errors.Is(err, siq.ErrSuspiciousRatio):
		return huma.Error413RequestEntityTooLarge(err.Error())
	case errors.Is(err, siq.ErrMediaNotFound):
		return huma.Error500InternalServerError(err.Error())

	// ai
	case errors.Is(err, ai.ErrNotConfigured):
		return huma.Error503ServiceUnavailable("AI judge is not configured: set OPENROUTER_API_KEY")
	case errors.Is(err, ai.ErrTimeout):
		return huma.Error504GatewayTimeout(err.Error())
	case errors.Is(err, ai.ErrUpstream), errors.Is(err, ai.ErrBadResponse):
		return huma.Error502BadGateway(err.Error())

	// context
	case errors.Is(err, context.DeadlineExceeded):
		return huma.Error504GatewayTimeout("request timed out")
	}
	return huma.Error500InternalServerError("internal error")
}

// fail maps err for a huma handler, logging server-side failures. In dev
// mode the original message is exposed in the 500 detail.
func (s *Server) fail(err error) error {
	mapped := mapError(err)
	var se huma.StatusError
	if errors.As(mapped, &se) && se.GetStatus() >= 500 {
		s.log.Error("http request failed", "err", err)
		if s.deps.Cfg.Dev {
			return huma.NewError(se.GetStatus(), err.Error())
		}
	}
	return mapped
}

// versionConflict builds the 409 for an optimistic-concurrency failure,
// reporting the current version in the detail and as an error entry.
func (s *Server) versionConflict(ctx context.Context, id string) error {
	sum, err := s.deps.Packs.GetSummary(ctx, id)
	if err != nil {
		return s.fail(err)
	}
	return huma.Error409Conflict(
		fmt.Sprintf("pack version conflict: the current version is %d", sum.Version),
		&huma.ErrorDetail{Location: "version", Message: "current pack version", Value: sum.Version})
}

// writeProblem writes err (mapped with mapError when needed) as
// application/problem+json from a plain chi handler.
func writeProblem(w http.ResponseWriter, err error) {
	mapped := mapError(err)
	var model *huma.ErrorModel
	if !errors.As(mapped, &model) {
		var se huma.StatusError
		status := http.StatusInternalServerError
		detail := "internal error"
		if errors.As(mapped, &se) {
			status = se.GetStatus()
			detail = se.Error()
		}
		model = &huma.ErrorModel{Status: status, Title: http.StatusText(status), Detail: detail}
	}
	if model.Title == "" {
		model.Title = http.StatusText(model.Status)
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(model.Status)
	_ = json.NewEncoder(w).Encode(model)
}

// writeJSON writes v as application/json with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
