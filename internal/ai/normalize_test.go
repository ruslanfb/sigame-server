package ai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Пушкин", "пушкин"},
		{"  Александр   Сергеевич  ПУШКИН ", "александр сергеевич пушкин"},
		{"Ёлка", "елка"},
		{"ёж", "еж"},
		{"Ё", "е"},
		{"Йошкар-Ола", "йошкар ола"},
		{"«Война и мир»", "война и мир"},
		{"Don't stop!", "don t stop"},
		{"Санкт-Петербург", "санкт петербург"},
		{"1812", "1812"},
		{"1 000", "1 000"},
		{"\u0438\u0306", "й"}, // и + combining breve → NFC й
		{"e\u0301", "é"},      // decomposed é → composed
		{"a\tb\nc", "a b c"},
		{"", ""},
		{"!!!", ""},
		{"  ...  ", ""},
		{"Rock 'n' Roll", "rock n roll"},
		{"Ёлки-палки!!!", "елки палки"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			require.Equal(t, c.want, Normalize(c.in))
		})
	}
}

func TestSimilarity(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"пушкин", "пушкин", 1},
		{"пушкин", "пушкен", 1 - 1.0/6},   // 0.833 ≥ 0.81
		{"толстой", "толстый", 1 - 1.0/7}, // 0.857
		{"париж", "парис", 0.8},           // just below the SIGame threshold
		{"лермонтов", "лермантов", 1 - 1.0/9},
		{"kitten", "sitting", 1 - 3.0/7},
		{"a", "", 0},
		{"", "", 1},
		{"abc", "xyz", 0},
		{"пушкин", "александр пушкин", 1 - 10.0/16},
	}
	for _, c := range cases {
		t.Run(c.a+"/"+c.b, func(t *testing.T) {
			require.InDelta(t, c.want, Similarity(c.a, c.b), 1e-9)
			require.InDelta(t, c.want, Similarity(c.b, c.a), 1e-9, "symmetry")
		})
	}
	th := DefaultThreshold
	require.GreaterOrEqual(t, Similarity("пушкин", "пушкен"), th)
	require.Less(t, Similarity("париж", "парис"), th)
}

func TestIsDigits(t *testing.T) {
	require.True(t, IsDigits("1812"))
	require.True(t, IsDigits("1 000"))
	require.False(t, IsDigits("1812 год"))
	require.False(t, IsDigits(""))
	require.False(t, IsDigits(" "))
	require.False(t, IsDigits("12a"))
}

func TestParseNumber(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"100", 100, true},
		{" 100 ", 100, true},
		{"1 000", 1000, true},
		{"1\u00a0000", 1000, true},
		{"1,000", 1000, true},
		{"1,5", 1.5, true},
		{"1.5", 1.5, true},
		{"-5", -5, true},
		{"\u22125", -5, true},
		{"+7", 7, true},
		{"1e3", 1000, true},
		{"abc", 0, false},
		{"", 0, false},
		{"1,2,3", 0, false},
		{"NaN", 0, false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, ok := ParseNumber(c.in)
			require.Equal(t, c.ok, ok)
			if ok {
				require.InDelta(t, c.want, got, 1e-9)
			}
		})
	}
}

func TestNumbersEqual(t *testing.T) {
	cases := []struct {
		a, b      string
		deviation float64
		want      bool
	}{
		{"100", "100", 0, true},
		{"103", "100", 5, true},
		{"105", "100", 5, true},
		{"106", "100", 5, false},
		{"95", "100", 5, true},
		{"94", "100", 5, false},
		{"1 000", "1000", 0, true},
		{"1,5", "1.5", 0, true},
		{"1,000", "1000", 0, true},
		{"100.0", "100", 0, true},
		{"-5", "\u22125", 0, true},
		{"abc", "100", 5, false},
		{"100", "", 5, false},
		{"100", "101", -1, false},
	}
	for _, c := range cases {
		t.Run(c.a+"~"+c.b, func(t *testing.T) {
			require.Equal(t, c.want, NumbersEqual(c.a, c.b, c.deviation))
		})
	}
}

func TestStripParens(t *testing.T) {
	require.Equal(t, "Пушкин ", stripParens("Пушкин (Александр Сергеевич)"))
	require.Equal(t, "a  d", stripParens("a (b [c]) d"))
	require.Equal(t, "plain", stripParens("plain"))
}
