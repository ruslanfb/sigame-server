package ai

import (
	"math"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Normalize canonicalizes an answer for comparison: Unicode NFC, lower case,
// ё→е (й is kept), every punctuation/symbol/quote/dash run becomes a single
// space, whitespace is collapsed and trimmed. Combining marks that survive NFC
// are dropped.
func Normalize(s string) string {
	s = strings.ToLower(norm.NFC.String(s))
	var b strings.Builder
	b.Grow(len(s))
	pendingSpace := false
	for _, r := range s {
		switch {
		case r == 'ё':
			r = 'е'
		case unicode.Is(unicode.Mn, r):
			continue
		case unicode.IsLetter(r) || unicode.IsDigit(r):
		default:
			pendingSpace = true
			continue
		}
		if pendingSpace && b.Len() > 0 {
			b.WriteByte(' ')
		}
		pendingSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

// Similarity returns 1 - levenshtein(a, b) / max(len(a), len(b)) over runes,
// i.e. 1 for equal strings and 0 for completely different ones. It does not
// normalize its inputs.
func Similarity(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	n := max(len(ra), len(rb))
	if n == 0 {
		return 1
	}
	return 1 - float64(levenshtein(ra, rb))/float64(n)
}

// levenshtein is the classic two-row edit distance.
func levenshtein(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// IsDigits reports whether s (already normalized) consists of digits only,
// possibly grouped with spaces. Such answers must match exactly.
func IsDigits(s string) bool {
	seen := false
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			seen = true
		case r == ' ':
		default:
			return false
		}
	}
	return seen
}

// ParseNumber parses a human-typed number: surrounding spaces, thousand
// separators (space, NBSP, underscore, or a comma followed by exactly three
// digits), a decimal comma, a leading + and the Unicode minus sign are accepted.
func ParseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '_' || r == '\u00a0' || r == '\u202f' {
			return -1
		}
		return r
	}, s)
	s = strings.ReplaceAll(s, "\u2212", "-")
	if i := strings.IndexByte(s, ','); i >= 0 && !strings.Contains(s, ".") && strings.Count(s, ",") == 1 {
		if tail := s[i+1:]; len(tail) == 3 && IsDigits(tail) {
			s = s[:i] + tail // 1,000 → 1000
		} else {
			s = s[:i] + "." + tail // 1,5 → 1.5
		}
	}
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// NumbersEqual reports whether a and b parse as numbers whose difference does
// not exceed deviation (negative deviation counts as 0).
func NumbersEqual(a, b string, deviation float64) bool {
	x, okA := ParseNumber(a)
	y, okB := ParseNumber(b)
	if !okA || !okB {
		return false
	}
	return math.Abs(x-y) <= max(deviation, 0)+1e-9
}

// tokens splits a normalized string into words.
func tokens(s string) []string { return strings.Fields(s) }

// stripParens removes parenthesized fragments: "Пушкин (Александр)" → "Пушкин ".
func stripParens(s string) string {
	if !strings.ContainsAny(s, "([") {
		return s
	}
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}
