// Package ai implements the answer judge of the AI showman for «Своя игра»:
// a deterministic SIGame-compatible judge (exact/fuzzy/number/select/point/client),
// an LLM judge backed by OpenRouter, and a Composite that combines them with a
// per-question cache and a fuzzy fallback when the LLM is unavailable.
//
// Privacy: the only data that ever leaves the server is the question text,
// its theme, the accepted and known-wrong answers, the showman's comments and
// the player's answer. Player names, room codes and media never do.
package ai

import "context"

// Answer types (mirror packs.AnswerType as plain strings so that this package
// does not depend on the packs model).
const (
	AnswerText   = "text"   // free text: exact/fuzzy/AI (default when empty)
	AnswerSelect = "select" // option label, compared case-insensitively
	AnswerNumber = "number" // number within ±Deviation of any accepted answer
	AnswerPoint  = "point"  // "x,y" within Deviation (Euclidean distance) of "x,y[,ratio]"
	AnswerClient = "client" // the client already decided: PlayerAnswer is "+"/"-" (right/wrong)
)

// Source values identify which component produced a Verdict.
const (
	SourceExact    = "exact"    // normalized equality with an accepted or a known-wrong answer
	SourceNumber   = "number"   // numeric tolerance (number and point answer types)
	SourceSelect   = "select"   // option label comparison
	SourceFuzzy    = "fuzzy"    // Levenshtein similarity against the accepted answers
	SourceAI       = "ai"       // LLM verdict via OpenRouter
	SourceFallback = "fallback" // fuzzy verdict used because the AI call failed or timed out
)

// Verdict is the outcome of judging one player answer.
type Verdict struct {
	Right     bool    `json:"right" doc:"Accept the answer"`
	Factor    float64 `json:"factor" doc:"Credit factor: 1 = full price, 0 = wrong, (0,1) = partial credit"`
	Uncertain bool    `json:"uncertain" doc:"The judge could not decide; Right is false. The room escalates to a human or applies the fuzzy result"`
	Reason    string  `json:"reason,omitempty" doc:"Short human-readable explanation"`
	Source    string  `json:"source" enum:"exact,number,select,fuzzy,ai,fallback" doc:"Component that produced the verdict"`
}

// Request describes one answer to judge. Only the fields listed in the package
// documentation are forwarded to the LLM.
type Request struct {
	QuestionID      string   `json:"questionId,omitempty" doc:"Cache key scope; empty disables caching"`
	Language        string   `json:"language,omitempty" doc:"Pack language hint, e.g. ru or en"`
	Theme           string   `json:"theme,omitempty"`
	QuestionText    string   `json:"questionText,omitempty" doc:"Plain-text question body (media omitted)"`
	Right           []string `json:"right" doc:"Accepted answers"`
	Wrong           []string `json:"wrong,omitempty" doc:"Known wrong answers"`
	AnswerType      string   `json:"answerType,omitempty" enum:",text,select,number,point,client" doc:"Empty = text"`
	Deviation       float64  `json:"deviation,omitempty" doc:"number: ± tolerance; point: distance tolerance"`
	PlayerAnswer    string   `json:"playerAnswer"`
	ShowmanComments string   `json:"showmanComments,omitempty" doc:"Pack author's notes for the showman"`
}

// Judge decides whether a player's answer is accepted.
type Judge interface {
	Judge(ctx context.Context, req Request) (Verdict, error)
}

// JudgeFunc adapts a plain function to the Judge interface.
type JudgeFunc func(ctx context.Context, req Request) (Verdict, error)

// Judge implements Judge.
func (f JudgeFunc) Judge(ctx context.Context, req Request) (Verdict, error) { return f(ctx, req) }

// IsText reports whether the answer type is judged as free text.
func IsText(answerType string) bool { return answerType == "" || answerType == AnswerText }

func rightVerdict(source, reason string) Verdict {
	return Verdict{Right: true, Factor: 1, Source: source, Reason: reason}
}

func wrongVerdict(source, reason string) Verdict {
	return Verdict{Source: source, Reason: reason}
}

func uncertainVerdict(source, reason string) Verdict {
	return Verdict{Uncertain: true, Source: source, Reason: reason}
}
