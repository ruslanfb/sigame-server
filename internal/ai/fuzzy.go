package ai

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
)

const (
	// DefaultThreshold is the SIGame letter-similarity threshold for auto-validation.
	DefaultThreshold = 0.81
	// UncertainFloor: a best similarity in [UncertainFloor, threshold) is reported
	// as Uncertain instead of a plain wrong answer.
	UncertainFloor = 0.6
	// DefaultPointDeviation is the minimum tolerance for point answers.
	DefaultPointDeviation = 0.02
)

// FuzzyJudge is the deterministic SIGame-compatible judge. It never fails and
// never does I/O.
//
// Text answers: normalized equality with an accepted answer (or with the
// accepted answer minus parenthesized parts) is right; equality with a known
// wrong answer is wrong; otherwise the best Levenshtein similarity against the
// accepted answers must reach Threshold (digits-only answers must match
// exactly) and beat the best similarity against the known wrong answers. The
// same words in a different order are right. A wrong verdict is marked
// Uncertain when the best similarity lies in [UncertainFloor, Threshold) or
// when every word of the player's answer (fuzzily) occurs in an accepted answer
// (surname-only answers, «1812» for «1812 год»), so that the room can escalate
// to a human instead of rejecting outright.
type FuzzyJudge struct {
	Threshold float64 // 0 → DefaultThreshold
}

func (f FuzzyJudge) threshold() float64 {
	if f.Threshold <= 0 || f.Threshold > 1 {
		return DefaultThreshold
	}
	return f.Threshold
}

// Judge implements Judge.
func (f FuzzyJudge) Judge(_ context.Context, req Request) (Verdict, error) {
	switch req.AnswerType {
	case AnswerNumber:
		return judgeNumber(req), nil
	case AnswerSelect:
		return judgeSelect(req), nil
	case AnswerPoint:
		return judgePoint(req), nil
	case AnswerClient:
		return judgeClient(req), nil
	default:
		return f.judgeText(req), nil
	}
}

// ExactMatch reports whether the player's text answer equals (after
// normalization) an accepted answer (right=true) or a known wrong answer
// (right=false). ok is false when neither matches.
func ExactMatch(req Request) (v Verdict, ok bool) {
	player := Normalize(req.PlayerAnswer)
	if player == "" {
		return wrongVerdict(SourceExact, "empty answer"), true
	}
	for _, r := range req.Right {
		for _, variant := range answerVariants(r) {
			if variant == player {
				return rightVerdict(SourceExact, "matches accepted answer «"+r+"»"), true
			}
		}
	}
	for _, w := range req.Wrong {
		for _, variant := range answerVariants(w) {
			if variant == player {
				return wrongVerdict(SourceExact, "matches known wrong answer «"+w+"»"), true
			}
		}
	}
	return Verdict{}, false
}

func (f FuzzyJudge) judgeText(req Request) Verdict {
	if v, ok := ExactMatch(req); ok {
		return v
	}
	th := f.threshold()
	player := Normalize(req.PlayerAnswer)

	bestRight, rightText := bestMatch(player, req.Right)
	bestWrong, wrongText := bestMatch(player, req.Wrong)

	switch {
	case bestRight >= th && bestRight > bestWrong:
		return rightVerdict(SourceFuzzy, fmt.Sprintf("similar to «%s» (%.2f)", rightText, bestRight))
	case bestRight >= th && bestRight == bestWrong:
		return uncertainVerdict(SourceFuzzy, fmt.Sprintf("equally similar to «%s» and known wrong «%s»", rightText, wrongText))
	case bestWrong >= th && bestWrong > bestRight:
		return wrongVerdict(SourceFuzzy, fmt.Sprintf("similar to known wrong answer «%s» (%.2f)", wrongText, bestWrong))
	}

	if wordsSubset(player, req.Right, th) {
		return uncertainVerdict(SourceFuzzy, "partial answer: every word occurs in an accepted answer")
	}
	if bestRight >= UncertainFloor && bestRight >= bestWrong {
		return uncertainVerdict(SourceFuzzy, fmt.Sprintf("weakly similar to «%s» (%.2f)", rightText, bestRight))
	}
	if rightText == "" {
		return wrongVerdict(SourceFuzzy, "no accepted answers to compare with")
	}
	return wrongVerdict(SourceFuzzy, fmt.Sprintf("best similarity to «%s» is %.2f", rightText, bestRight))
}

// answerVariants returns the normalized forms of an answer to compare against:
// the full text and, when it contains parentheses, the text without them.
// Word order variants are handled separately.
func answerVariants(answer string) []string {
	full := Normalize(answer)
	if full == "" {
		return nil
	}
	variants := []string{full}
	if short := Normalize(stripParens(answer)); short != "" && short != full {
		variants = append(variants, short)
	}
	return variants
}

// bestMatch returns the best similarity between the normalized player answer
// and any variant of the candidates, plus the original candidate text.
// Digits-only candidates only match exactly. The same multiset of words in a
// different order counts as a full match.
func bestMatch(player string, candidates []string) (float64, string) {
	best, bestText := 0.0, ""
	playerWords := sortedWords(player)
	for _, c := range candidates {
		for _, variant := range answerVariants(c) {
			var sim float64
			switch {
			case variant == player:
				sim = 1
			case IsDigits(variant) || IsDigits(player):
				sim = 0
			case slices.Equal(playerWords, sortedWords(variant)):
				sim = 1
			default:
				sim = Similarity(player, variant)
			}
			if sim > best || bestText == "" {
				best, bestText = sim, c
			}
		}
	}
	return best, bestText
}

func sortedWords(s string) []string {
	w := tokens(s)
	slices.Sort(w)
	return w
}

// wordsSubset reports whether the player's answer is a strict subset of the
// words of some accepted answer, matching words fuzzily (≥ threshold; digits
// exactly). At least one player word must be 3+ runes long.
func wordsSubset(player string, candidates []string, threshold float64) bool {
	pw := tokens(player)
	if len(pw) == 0 || !slices.ContainsFunc(pw, func(w string) bool { return len([]rune(w)) >= 3 }) {
		return false
	}
	for _, c := range candidates {
		cw := tokens(Normalize(c))
		if len(cw) <= len(pw) {
			continue
		}
		all := true
		for _, w := range pw {
			if !slices.ContainsFunc(cw, func(cwd string) bool { return wordMatch(w, cwd, threshold) }) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func wordMatch(a, b string, threshold float64) bool {
	if a == b {
		return true
	}
	if IsDigits(a) || IsDigits(b) || len([]rune(a)) < 4 || len([]rune(b)) < 4 {
		return false
	}
	return Similarity(a, b) >= threshold
}

func judgeNumber(req Request) Verdict {
	if _, ok := ParseNumber(req.PlayerAnswer); !ok {
		return wrongVerdict(SourceNumber, "not a number")
	}
	for _, r := range req.Right {
		if NumbersEqual(req.PlayerAnswer, r, req.Deviation) {
			return rightVerdict(SourceNumber, fmt.Sprintf("within ±%g of %s", max(req.Deviation, 0), strings.TrimSpace(r)))
		}
	}
	return wrongVerdict(SourceNumber, fmt.Sprintf("outside ±%g of the accepted number", max(req.Deviation, 0)))
}

func judgeSelect(req Request) Verdict {
	player := strings.TrimSpace(req.PlayerAnswer)
	if player == "" {
		return wrongVerdict(SourceSelect, "no option selected")
	}
	for _, r := range req.Right {
		if strings.EqualFold(player, strings.TrimSpace(r)) {
			return rightVerdict(SourceSelect, "option "+strings.TrimSpace(r))
		}
	}
	return wrongVerdict(SourceSelect, "option "+player+" is not the right one")
}

// judgePoint compares "x,y" with "x,y[,ratio]" by Euclidean distance in the
// image's normalized coordinates; the ratio component is ignored.
func judgePoint(req Request) Verdict {
	px, py, ok := parsePoint(req.PlayerAnswer)
	if !ok {
		return wrongVerdict(SourceNumber, "not a point")
	}
	tol := max(req.Deviation, DefaultPointDeviation)
	for _, r := range req.Right {
		rx, ry, ok := parsePoint(r)
		if !ok {
			continue
		}
		if d := math.Hypot(px-rx, py-ry); d <= tol+1e-9 {
			return rightVerdict(SourceNumber, fmt.Sprintf("distance %.3f ≤ %g", d, tol))
		}
	}
	return wrongVerdict(SourceNumber, fmt.Sprintf("farther than %g from the accepted point", tol))
}

func parsePoint(s string) (x, y float64, ok bool) {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' })
	if len(parts) < 2 {
		return 0, 0, false
	}
	x, okX := ParseNumber(parts[0])
	y, okY := ParseNumber(parts[1])
	return x, y, okX && okY
}

// judgeClient trusts the client's own decision carried in PlayerAnswer.
func judgeClient(req Request) Verdict {
	switch strings.ToLower(strings.TrimSpace(req.PlayerAnswer)) {
	case "+", "right", "true", "1", "yes", "correct":
		return rightVerdict(SourceExact, "client reported a right answer")
	case "-", "wrong", "false", "0", "no", "incorrect", "":
		return wrongVerdict(SourceExact, "client reported a wrong answer")
	default:
		return uncertainVerdict(SourceExact, "unrecognized client verdict «"+strings.TrimSpace(req.PlayerAnswer)+"»")
	}
}
