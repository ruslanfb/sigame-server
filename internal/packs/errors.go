package packs

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors returned by Repo.
var (
	ErrNotFound        = errors.New("packs: not found")
	ErrAlreadyExists   = errors.New("packs: already exists")
	ErrVersionConflict = errors.New("packs: version conflict")
)

// Problem codes used in ValidationError. The HTTP layer maps them 1:1 into
// RFC 9457 error details.
const (
	CodeRequired      = "required"        // a mandatory value is missing
	CodeTooLong       = "tooLong"         // string exceeds its length limit
	CodeTooMany       = "tooMany"         // list exceeds its item limit
	CodeTooFew        = "tooFew"          // list has fewer items than required
	CodeInvalidValue  = "invalidValue"    // value is not one of the allowed ones / malformed
	CodeOutOfRange    = "outOfRange"      // number outside its allowed range
	CodeUnexpected    = "unexpectedField" // value must be empty for this type
	CodeDuplicate     = "duplicate"       // duplicate entry in a list
	CodeMismatch      = "mismatch"        // related values disagree
	CodeMediaNotFound = "mediaNotFound"   // referenced media id is not stored on this server
)

// Problem is one validation finding.
type Problem struct {
	Path    string `json:"path" doc:"JSON pointer to the offending value, e.g. /rounds/0/themes/2/questions/1/price"`
	Code    string `json:"code" doc:"Machine-readable code: required|tooLong|tooMany|tooFew|invalidValue|outOfRange|unexpectedField|duplicate|mismatch|mediaNotFound"`
	Message string `json:"message" doc:"Human-readable explanation"`
}

// ValidationError lists every problem found in a pack. It is returned as
// *ValidationError so that callers can use errors.As.
type ValidationError struct {
	Problems []Problem `json:"problems"`
}

// Error renders a compact message: the first three problems and a count of
// the rest.
func (e *ValidationError) Error() string {
	const shown = 3
	var b strings.Builder
	fmt.Fprintf(&b, "packs: validation failed (%d problem", len(e.Problems))
	if len(e.Problems) != 1 {
		b.WriteString("s")
	}
	b.WriteString(")")
	for i, p := range e.Problems {
		if i >= shown {
			fmt.Fprintf(&b, "; and %d more", len(e.Problems)-shown)
			break
		}
		if i == 0 {
			b.WriteString(": ")
		} else {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s: %s (%s)", p.Path, p.Message, p.Code)
	}
	return b.String()
}

// add appends a problem.
func (e *ValidationError) add(path, code, format string, args ...any) {
	e.Problems = append(e.Problems, Problem{Path: path, Code: code, Message: fmt.Sprintf(format, args...)})
}

// orNil returns nil when no problem was recorded, so that callers can return
// the result directly as an error value without the typed-nil pitfall.
func (e *ValidationError) orNil() error {
	if e == nil || len(e.Problems) == 0 {
		return nil
	}
	return e
}
