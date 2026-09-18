package ai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFuzzyJudgeTable(t *testing.T) {
	type want struct {
		right, uncertain bool
		source           string
	}
	cases := []struct {
		name string
		req  Request
		want want
	}{
		{"exact", Request{Right: []string{"Пушкин"}, PlayerAnswer: "пушкин"}, want{true, false, SourceExact}},
		{"typo above threshold", Request{Right: []string{"Лермонтов"}, PlayerAnswer: "Лермантов"}, want{true, false, SourceFuzzy}},
		{"just below threshold is uncertain", Request{Right: []string{"Париж"}, PlayerAnswer: "Парис"}, want{false, true, SourceFuzzy}},
		{"surname only is uncertain", Request{Right: []string{"Александр Пушкин"}, PlayerAnswer: "Пушкин"}, want{false, true, SourceFuzzy}},
		{"transliteration miss is wrong", Request{Right: []string{"Пушкин"}, PlayerAnswer: "Pushkin"}, want{false, false, SourceFuzzy}},
		{"known wrong exact", Request{Right: []string{"Толстой"}, Wrong: []string{"Толстый"}, PlayerAnswer: "Толстый"}, want{false, false, SourceExact}},
		{"closer to known wrong", Request{Right: []string{"Толстой"}, Wrong: []string{"Толстый"}, PlayerAnswer: "толстыи"}, want{false, false, SourceFuzzy}},
		{"closer to right despite wrong list", Request{Right: []string{"Толстой"}, Wrong: []string{"Толстый"}, PlayerAnswer: "толстои"}, want{true, false, SourceFuzzy}},
		{"digits exact", Request{Right: []string{"1812"}, PlayerAnswer: "1812"}, want{true, false, SourceExact}},
		{"digits near miss is wrong", Request{Right: []string{"1812"}, PlayerAnswer: "1813"}, want{false, false, SourceFuzzy}},
		{"number without unit is uncertain", Request{Right: []string{"1812 год"}, PlayerAnswer: "1812"}, want{false, true, SourceFuzzy}},
		{"word order", Request{Right: []string{"Александр Пушкин"}, PlayerAnswer: "Пушкин Александр"}, want{true, false, SourceFuzzy}},
		{"parenthesized part optional", Request{Right: []string{"Пушкин (Александр Сергеевич)"}, PlayerAnswer: "Пушкин"}, want{true, false, SourceExact}},
		{"empty answer", Request{Right: []string{"Пушкин"}, PlayerAnswer: "   "}, want{false, false, SourceExact}},
		{"punctuation and case", Request{Right: []string{"Война и мир"}, PlayerAnswer: "«ВОЙНА И МИР!»"}, want{true, false, SourceExact}},
		{"ё vs е", Request{Right: []string{"Ёлка"}, PlayerAnswer: "елка"}, want{true, false, SourceExact}},
		{"any of several right answers", Request{Right: []string{"Санкт-Петербург", "Питер", "Ленинград"}, PlayerAnswer: "питер"}, want{true, false, SourceExact}},
		{"different entity", Request{Right: []string{"Пушкин"}, PlayerAnswer: "Гоголь"}, want{false, false, SourceFuzzy}},
		{"english surname with typo is uncertain", Request{Right: []string{"William Shakespeare"}, PlayerAnswer: "Shakespear"}, want{false, true, SourceFuzzy}},
		{"hedging two candidates is wrong", Request{Right: []string{"Пушкин"}, PlayerAnswer: "Пушкин или Лермонтов"}, want{false, false, SourceFuzzy}},
		{"extra word lands in the uncertain band", Request{Right: []string{"Пушкин"}, PlayerAnswer: "это Пушкин"}, want{false, true, SourceFuzzy}},
		{"tie between right and wrong", Request{Right: []string{"абвгде"}, Wrong: []string{"абвгдя"}, PlayerAnswer: "абвгдю"}, want{false, true, SourceFuzzy}},
		{"no right answers", Request{PlayerAnswer: "что-то"}, want{false, false, SourceFuzzy}},
		{"select right label case-insensitive", Request{AnswerType: AnswerSelect, Right: []string{"B"}, PlayerAnswer: " b "}, want{true, false, SourceSelect}},
		{"select wrong label", Request{AnswerType: AnswerSelect, Right: []string{"B"}, PlayerAnswer: "A"}, want{false, false, SourceSelect}},
		{"select empty", Request{AnswerType: AnswerSelect, Right: []string{"B"}, PlayerAnswer: ""}, want{false, false, SourceSelect}},
		{"number within deviation", Request{AnswerType: AnswerNumber, Right: []string{"100"}, Deviation: 5, PlayerAnswer: "104"}, want{true, false, SourceNumber}},
		{"number at the edge", Request{AnswerType: AnswerNumber, Right: []string{"100"}, Deviation: 5, PlayerAnswer: "95"}, want{true, false, SourceNumber}},
		{"number outside deviation", Request{AnswerType: AnswerNumber, Right: []string{"100"}, Deviation: 5, PlayerAnswer: "106"}, want{false, false, SourceNumber}},
		{"number not numeric", Request{AnswerType: AnswerNumber, Right: []string{"100"}, Deviation: 5, PlayerAnswer: "сто"}, want{false, false, SourceNumber}},
		{"point within tolerance", Request{AnswerType: AnswerPoint, Right: []string{"0.5,0.5,1.33"}, Deviation: 0.05, PlayerAnswer: "0.52,0.49"}, want{true, false, SourceNumber}},
		{"point outside tolerance", Request{AnswerType: AnswerPoint, Right: []string{"0.5,0.5,1.33"}, Deviation: 0.05, PlayerAnswer: "0.7,0.7"}, want{false, false, SourceNumber}},
		{"point default tolerance", Request{AnswerType: AnswerPoint, Right: []string{"0.5;0.5"}, PlayerAnswer: "0.51 0.505"}, want{true, false, SourceNumber}},
		{"point garbage", Request{AnswerType: AnswerPoint, Right: []string{"0.5,0.5"}, PlayerAnswer: "here"}, want{false, false, SourceNumber}},
		{"client plus", Request{AnswerType: AnswerClient, Right: []string{"x"}, PlayerAnswer: "+"}, want{true, false, SourceExact}},
		{"client minus", Request{AnswerType: AnswerClient, Right: []string{"x"}, PlayerAnswer: "-"}, want{false, false, SourceExact}},
		{"client unknown", Request{AnswerType: AnswerClient, Right: []string{"x"}, PlayerAnswer: "maybe"}, want{false, true, SourceExact}},
	}
	j := FuzzyJudge{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, err := j.Judge(context.Background(), c.req)
			require.NoError(t, err)
			require.Equal(t, c.want.right, v.Right, "right; reason: %s", v.Reason)
			require.Equal(t, c.want.uncertain, v.Uncertain, "uncertain; reason: %s", v.Reason)
			require.Equal(t, c.want.source, v.Source, "source")
			require.NotEmpty(t, v.Reason)
			if v.Right {
				require.Equal(t, 1.0, v.Factor)
			} else {
				require.Equal(t, 0.0, v.Factor)
				require.False(t, v.Uncertain && v.Right)
			}
		})
	}
}

func TestFuzzyJudgeThreshold(t *testing.T) {
	ctx := context.Background()
	req := Request{Right: []string{"Париж"}, PlayerAnswer: "Парис"} // similarity 0.8

	v, err := FuzzyJudge{}.Judge(ctx, req)
	require.NoError(t, err)
	require.False(t, v.Right)
	require.True(t, v.Uncertain)

	v, err = FuzzyJudge{Threshold: 0.7}.Judge(ctx, req)
	require.NoError(t, err)
	require.True(t, v.Right)
	require.Equal(t, SourceFuzzy, v.Source)

	// Out-of-range thresholds fall back to the default.
	require.Equal(t, DefaultThreshold, FuzzyJudge{Threshold: 1.5}.threshold())
	require.Equal(t, DefaultThreshold, FuzzyJudge{}.threshold())
}

func TestExactMatch(t *testing.T) {
	v, ok := ExactMatch(Request{Right: []string{"Пушкин"}, PlayerAnswer: "ПУШКИН."})
	require.True(t, ok)
	require.True(t, v.Right)

	v, ok = ExactMatch(Request{Right: []string{"Пушкин"}, Wrong: []string{"Лермонтов"}, PlayerAnswer: "лермонтов"})
	require.True(t, ok)
	require.False(t, v.Right)
	require.Equal(t, SourceExact, v.Source)

	_, ok = ExactMatch(Request{Right: []string{"Пушкин"}, PlayerAnswer: "Гоголь"})
	require.False(t, ok)
}
