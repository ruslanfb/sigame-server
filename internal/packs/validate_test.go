package packs

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// hex64 builds a syntactically valid media id.
func hex64(c byte) string { return strings.Repeat(string(c), 64) }

var (
	mediaA = hex64('a')
	mediaB = hex64('b')
	mediaC = hex64('c')
)

// samplePack is a valid pack exercising every question kind used in tests.
// Fields with omitempty are either nil or non-empty so that a JSON
// round-trip yields an equal struct.
func samplePack() *Pack {
	return &Pack{
		Name:        "Sample pack",
		Language:    "ru-RU",
		Difficulty:  3,
		Restriction: "12+",
		Tags:        []string{"history", "Science"},
		Info:        Info{Authors: []string{"Alice", "Bob"}, Comments: "hello"},
		Rounds: []Round{
			{Name: "Round 1", Type: RoundStandard, Themes: []Theme{
				{Name: "History", Questions: []Question{
					{Price: 100, Type: QSimple,
						Params: QuestionParams{Question: []ContentItem{{Type: ContentText, Text: "Who?"}}},
						Right:  []string{"Napoleon"}, Wrong: []string{"Caesar"}},
					{Price: 200, Type: QSecret,
						Params: QuestionParams{
							Question:      []ContentItem{{Type: ContentImage, MediaID: mediaA}, {Type: ContentAudio, MediaID: mediaB}},
							Theme:         "Secret theme",
							Price:         &NumberSet{Min: 100, Max: 500, Step: 100},
							SelectionMode: SelectExceptCurrent,
						},
						Right: []string{"x"}},
					{Price: 300, Type: QSimple,
						Params: QuestionParams{
							Question:   []ContentItem{{Type: ContentText, Text: "Pick"}},
							AnswerType: AnswerSelect,
							AnswerOptions: []AnswerOption{
								{Label: "A", Content: []ContentItem{{Type: ContentText, Text: "one"}}},
								{Label: "B", Content: []ContentItem{{Type: ContentText, Text: "two"}}},
							},
						},
						Right: []string{"B"}},
					{Price: 400, Type: QForAll,
						Params: QuestionParams{
							Question:        []ContentItem{{Type: ContentText, Text: "How many?"}},
							AnswerType:      AnswerNumber,
							AnswerDeviation: 2,
						},
						Right: []string{"42"}},
					{Price: -1},
				}},
			}},
			{Name: "Final", Type: RoundFinal, Themes: []Theme{
				{Name: "Final theme", Questions: []Question{
					{Price: 0, Type: QStakeAll,
						Params: QuestionParams{Question: []ContentItem{{Type: ContentVideo, URL: "https://example.com/v.mp4"}}},
						Right:  []string{"final"}},
				}},
			}},
		},
	}
}

// q returns the first standard-round question at index k.
func q(p *Pack, k int) *Question { return &p.Rounds[0].Themes[0].Questions[k] }

func problems(t *testing.T, err error) []Problem {
	t.Helper()
	var ve *ValidationError
	require.ErrorAs(t, err, &ve, "expected *ValidationError, got %v", err)
	return ve.Problems
}

func requireProblem(t *testing.T, err error, path, code string) {
	t.Helper()
	ps := problems(t, err)
	for _, p := range ps {
		if p.Path == path && p.Code == code {
			return
		}
	}
	t.Fatalf("no problem %s %s in %+v", path, code, ps)
}

func TestValidate_Table(t *testing.T) {
	const qp = "/rounds/0/themes/0/questions/0"
	cases := []struct {
		name   string
		mutate func(p *Pack)
		path   string // "" = expect valid
		code   string
	}{
		{"valid sample", func(p *Pack) {}, "", ""},
		{"name required", func(p *Pack) { p.Name = "  " }, "/name", CodeRequired},
		{"name too long", func(p *Pack) { p.Name = strings.Repeat("я", MaxTextLen+1) }, "/name", CodeTooLong},
		{"name at limit is fine", func(p *Pack) { p.Name = strings.Repeat("я", MaxTextLen) }, "", ""},
		{"difficulty out of range", func(p *Pack) { p.Difficulty = 11 }, "/difficulty", CodeOutOfRange},
		{"too many tags", func(p *Pack) { p.Tags = make([]string, MaxListItems+1) }, "/tags", CodeTooMany},
		{"too many authors", func(p *Pack) { p.Info.Authors = make([]string, MaxListItems+1) }, "/info/authors", CodeTooMany},
		{"logo media id malformed", func(p *Pack) { p.LogoMediaID = "abc" }, "/logoMediaId", CodeInvalidValue},
		{"logo url and id together", func(p *Pack) { p.LogoMediaID = mediaA; p.LogoURL = "https://x.y/l.png" }, "/logoUrl", CodeUnexpected},
		{"too many rounds", func(p *Pack) { p.Rounds = make([]Round, MaxRounds+1) }, "/rounds", CodeTooMany},
		{"round type invalid", func(p *Pack) { p.Rounds[0].Type = "bogus" }, "/rounds/0/type", CodeInvalidValue},
		{"round type empty is standard", func(p *Pack) { p.Rounds[0].Type = "" }, "", ""},
		{"too many themes", func(p *Pack) { p.Rounds[0].Themes = make([]Theme, MaxThemesPerRound+1) }, "/rounds/0/themes", CodeTooMany},
		{"too many questions", func(p *Pack) {
			qs := make([]Question, MaxQuestionsPerTheme+1)
			for i := range qs {
				qs[i] = *q(p, 0)
			}
			p.Rounds[0].Themes[0].Questions = qs
		}, "/rounds/0/themes/0/questions", CodeTooMany},
		{"final theme without questions", func(p *Pack) { p.Rounds[1].Themes[0].Questions = nil }, "/rounds/1/themes/0/questions", CodeTooFew},
		{"price below -1", func(p *Pack) { q(p, 0).Price = -2 }, qp + "/price", CodeOutOfRange},
		{"custom question type is fine", func(p *Pack) { q(p, 0).Type = "myOwnScript" }, "", ""},
		{"no content", func(p *Pack) { q(p, 0).Params.Question = nil }, qp + "/params/question", CodeTooFew},
		{"secretNoQuestion needs no content or answer", func(p *Pack) {
			q(p, 0).Type = QSecretNoQuestion
			q(p, 0).Params.Question = nil
			q(p, 0).Right = nil
		}, "", ""},
		{"empty slot needs nothing", func(p *Pack) { q(p, 0).Params.Question = nil; q(p, 0).Right = nil; q(p, 0).Price = -1 }, "", ""},
		{"too many items", func(p *Pack) {
			items := make([]ContentItem, MaxItemsPerParam+1)
			for i := range items {
				items[i] = ContentItem{Type: ContentText, Text: "t"}
			}
			q(p, 0).Params.Question = items
		}, qp + "/params/question", CodeTooMany},
		{"right required", func(p *Pack) { q(p, 0).Right = nil }, qp + "/right", CodeTooFew},
		{"right may be empty string", func(p *Pack) { q(p, 0).Right = []string{""} }, "", ""},
		{"too many right answers", func(p *Pack) { q(p, 0).Right = make([]string, MaxAnswers+1) }, qp + "/right", CodeTooMany},
		{"answer type invalid", func(p *Pack) { q(p, 0).Params.AnswerType = "bogus" }, qp + "/params/answerType", CodeInvalidValue},
		{"select needs two options", func(p *Pack) { q(p, 2).Params.AnswerOptions = q(p, 2).Params.AnswerOptions[:1] },
			"/rounds/0/themes/0/questions/2/params/answerOptions", CodeTooFew},
		{"select duplicate labels", func(p *Pack) { q(p, 2).Params.AnswerOptions[1].Label = "a" },
			"/rounds/0/themes/0/questions/2/params/answerOptions/1/label", CodeDuplicate},
		{"select empty label", func(p *Pack) { q(p, 2).Params.AnswerOptions[1].Label = " " },
			"/rounds/0/themes/0/questions/2/params/answerOptions/1/label", CodeRequired},
		{"select right must be a label", func(p *Pack) { q(p, 2).Right = []string{"Z"} },
			"/rounds/0/themes/0/questions/2/right/0", CodeMismatch},
		{"select right is case-insensitive", func(p *Pack) { q(p, 2).Right = []string{"b"} }, "", ""},
		{"number right must parse", func(p *Pack) { q(p, 3).Right = []string{"forty-two"} },
			"/rounds/0/themes/0/questions/3/right/0", CodeInvalidValue},
		{"number with decimal comma", func(p *Pack) { q(p, 3).Right = []string{"12,5"} }, "", ""},
		{"deviation negative", func(p *Pack) { q(p, 3).Params.AnswerDeviation = -1 },
			"/rounds/0/themes/0/questions/3/params/answerDeviation", CodeOutOfRange},
		{"secret min > max", func(p *Pack) { q(p, 1).Params.Price = &NumberSet{Min: 500, Max: 100} },
			"/rounds/0/themes/0/questions/1/params/price/min", CodeMismatch},
		{"secret step does not divide", func(p *Pack) { q(p, 1).Params.Price = &NumberSet{Min: 100, Max: 500, Step: 300} },
			"/rounds/0/themes/0/questions/1/params/price/step", CodeMismatch},
		{"secret step negative", func(p *Pack) { q(p, 1).Params.Price = &NumberSet{Min: 0, Max: 0, Step: -1} },
			"/rounds/0/themes/0/questions/1/params/price/step", CodeOutOfRange},
		{"secret fixed price", func(p *Pack) { q(p, 1).Params.Price = &NumberSet{Min: 300, Max: 300} }, "", ""},
		{"selection mode invalid", func(p *Pack) { q(p, 1).Params.SelectionMode = "bogus" },
			"/rounds/0/themes/0/questions/1/params/selectionMode", CodeInvalidValue},
		{"secret params on simple are ignored", func(p *Pack) {
			q(p, 0).Params.Price = &NumberSet{Min: 100, Max: 200, Step: 50}
			q(p, 0).Params.SelectionMode = SelectAny
			q(p, 0).Params.Theme = "extra"
		}, "", ""},
		{"text item without text", func(p *Pack) { q(p, 0).Params.Question[0].Text = " " }, qp + "/params/question/0/text", CodeRequired},
		{"text item too long", func(p *Pack) { q(p, 0).Params.Question[0].Text = strings.Repeat("x", MaxContentValueLen+1) },
			qp + "/params/question/0/text", CodeTooLong},
		{"text item with media", func(p *Pack) { q(p, 0).Params.Question[0].MediaID = mediaA }, qp + "/params/question/0/mediaId", CodeUnexpected},
		{"content type invalid", func(p *Pack) { q(p, 0).Params.Question[0].Type = "gif" }, qp + "/params/question/0/type", CodeInvalidValue},
		{"image without source", func(p *Pack) { q(p, 0).Params.Question[0] = ContentItem{Type: ContentImage} },
			qp + "/params/question/0/mediaId", CodeRequired},
		{"image with both sources", func(p *Pack) {
			q(p, 0).Params.Question[0] = ContentItem{Type: ContentImage, MediaID: mediaA, URL: "https://a.b/c.png"}
		}, qp + "/params/question/0/url", CodeUnexpected},
		{"media id not sha256", func(p *Pack) { q(p, 0).Params.Question[0] = ContentItem{Type: ContentImage, MediaID: "ABC"} },
			qp + "/params/question/0/mediaId", CodeInvalidValue},
		{"media id upper-case hex", func(p *Pack) {
			q(p, 0).Params.Question[0] = ContentItem{Type: ContentImage, MediaID: strings.ToUpper(mediaA)}
		},
			qp + "/params/question/0/mediaId", CodeInvalidValue},
		{"url scheme invalid", func(p *Pack) { q(p, 0).Params.Question[0] = ContentItem{Type: ContentVideo, URL: "ftp://x.y/z"} },
			qp + "/params/question/0/url", CodeInvalidValue},
		{"url relative", func(p *Pack) { q(p, 0).Params.Question[0] = ContentItem{Type: ContentVideo, URL: "/z.mp4"} },
			qp + "/params/question/0/url", CodeInvalidValue},
		{"media item with text", func(p *Pack) {
			q(p, 0).Params.Question[0] = ContentItem{Type: ContentImage, MediaID: mediaA, Text: "x"}
		},
			qp + "/params/question/0/text", CodeUnexpected},
		{"replic on image", func(p *Pack) {
			q(p, 0).Params.Question[0] = ContentItem{Type: ContentImage, MediaID: mediaA, Placement: PlaceReplic}
		}, qp + "/params/question/0/placement", CodeMismatch},
		{"background on text", func(p *Pack) { q(p, 0).Params.Question[0].Placement = PlaceBackground },
			qp + "/params/question/0/placement", CodeMismatch},
		{"placement invalid", func(p *Pack) { q(p, 0).Params.Question[0].Placement = "sky" },
			qp + "/params/question/0/placement", CodeInvalidValue},
		{"replic on text is fine", func(p *Pack) { q(p, 0).Params.Question[0].Placement = PlaceReplic }, "", ""},
		{"duration negative", func(p *Pack) { q(p, 0).Params.Question[0].DurationMs = -5 },
			qp + "/params/question/0/durationMs", CodeOutOfRange},
		{"answer items validated", func(p *Pack) { q(p, 0).Params.Answer = []ContentItem{{Type: ContentAudio}} },
			qp + "/params/answer/0/mediaId", CodeRequired},
		{"option content validated", func(p *Pack) { q(p, 2).Params.AnswerOptions[0].Content = []ContentItem{{Type: ContentText}} },
			"/rounds/0/themes/0/questions/2/params/answerOptions/0/content/0/text", CodeRequired},
		{"script step without type", func(p *Pack) { q(p, 0).Script = []ScriptStep{{Params: []Param{}}} }, qp + "/script/0/type", CodeRequired},
		{"too many script steps", func(p *Pack) {
			steps := make([]ScriptStep, MaxScriptSteps+1)
			for i := range steps {
				steps[i] = ScriptStep{Type: "setAnswerer"}
			}
			q(p, 0).Script = steps
		}, qp + "/script", CodeTooMany},
		{"extra param items validated", func(p *Pack) {
			q(p, 0).Extra = []Param{{Name: "x", Type: "content", Items: []ContentItem{{Type: ContentImage, MediaID: "zz"}}}}
		}, qp + "/extraParams/0/items/0/mediaId", CodeInvalidValue},
		{"extra param needs name", func(p *Pack) { q(p, 0).Extra = []Param{{Value: "v"}} }, qp + "/extraParams/0/name", CodeRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := samplePack()
			tc.mutate(p)
			err := Validate(p)
			if tc.path == "" {
				require.NoError(t, err)
				return
			}
			requireProblem(t, err, tc.path, tc.code)
		})
	}
}

func TestValidate_NilPackAndMultipleProblems(t *testing.T) {
	require.Error(t, Validate(nil))

	p := samplePack()
	p.Name = ""
	p.Difficulty = 42
	p.Rounds[0].Type = "x"
	q(p, 0).Price = -7
	q(p, 1).Right = nil
	err := Validate(p)
	ps := problems(t, err)
	require.Len(t, ps, 5)
	msg := err.Error()
	require.Contains(t, msg, "5 problems")
	require.Contains(t, msg, "/name")
	require.Contains(t, msg, "and 2 more")

	single := &ValidationError{Problems: []Problem{{Path: "/name", Code: CodeRequired, Message: "value is required"}}}
	require.Equal(t, "packs: validation failed (1 problem): /name: value is required (required)", single.Error())
	var target *ValidationError
	require.True(t, errors.As(error(single), &target))
}

func TestNormalize_Canonical(t *testing.T) {
	p := &Pack{
		Name: "  Pack  ",
		Tags: []string{" a ", "", "b"},
		Info: Info{Authors: []string{" me ", " "}},
		Rounds: []Round{{Name: " R ", Themes: []Theme{{Name: " T ", Questions: []Question{
			{Params: QuestionParams{
				AnswerType:    AnswerSelect,
				AnswerOptions: []AnswerOption{{Label: " a "}, {Label: "b"}},
				Question:      []ContentItem{{Type: ContentImage, MediaID: " " + strings.ToUpper(mediaA) + " "}, {Type: ContentAudio, MediaID: mediaB}},
			}, Right: []string{" A "}, Script: []ScriptStep{{Type: "step"}}},
			{},
		}}}}},
	}
	Normalize(p)
	require.Equal(t, "Pack", p.Name)
	require.Equal(t, []string{"a", "b"}, p.Tags)
	require.Equal(t, []string{"me"}, p.Info.Authors)
	require.Equal(t, DefaultDifficulty, p.Difficulty, "new pack gets the default difficulty")
	require.Equal(t, "R", p.Rounds[0].Name)
	require.Equal(t, RoundStandard, p.Rounds[0].Type)
	require.Equal(t, "T", p.Rounds[0].Themes[0].Name)

	q0 := p.Rounds[0].Themes[0].Questions[0]
	require.Equal(t, QSimple, q0.Type)
	require.Equal(t, "A", q0.Params.AnswerOptions[0].Label)
	require.Equal(t, "B", q0.Params.AnswerOptions[1].Label)
	require.NotNil(t, q0.Params.AnswerOptions[0].Content)
	require.Equal(t, mediaA, q0.Params.Question[0].MediaID)
	require.Equal(t, Placement(""), q0.Params.Question[1].Placement, "audio placement stays empty")
	require.Equal(t, PlaceBackground, q0.Params.Question[1].EffectivePlacement())
	require.Equal(t, []string{"A"}, q0.Right)
	require.NotNil(t, q0.Script[0].Params)

	q1 := p.Rounds[0].Themes[0].Questions[1]
	require.Equal(t, QSimple, q1.Type)
	require.Equal(t, []string{""}, q1.Right)
	require.NotNil(t, q1.Params.Question)

	// A persisted pack keeps an explicit difficulty 0.
	old := &Pack{Name: "x", Version: 3, CreatedAt: 1}
	Normalize(old)
	require.Equal(t, 0, old.Difficulty)

	Normalize(nil) // must not panic
}

func TestNormalize_JSONHasNoNullSlices(t *testing.T) {
	p := &Pack{Name: "x", Rounds: []Round{
		{Themes: []Theme{{Questions: []Question{
			{},
			{Params: QuestionParams{AnswerOptions: []AnswerOption{{Label: "a"}}}, Script: []ScriptStep{{Type: "s"}}},
		}}}},
		{},
	}}
	Normalize(p)
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "null")
	require.Contains(t, string(raw), `"tags":[]`)
	require.Contains(t, string(raw), `"themes":[]`)
	require.Contains(t, string(raw), `"question":[]`)
	require.Contains(t, string(raw), `"right":[""]`)
	require.Contains(t, string(raw), `"content":[]`)

	empty := &Pack{Name: "y"}
	Normalize(empty)
	raw, err = json.Marshal(empty)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "null")
	require.Contains(t, string(raw), `"rounds":[]`)
}

func TestEnsureIDsAndResetIDs(t *testing.T) {
	p := samplePack()
	p.Rounds[0].ID = "keep-me"
	EnsureIDs(p)
	require.NotEmpty(t, p.ID)
	require.Equal(t, "keep-me", p.Rounds[0].ID)
	require.NotEmpty(t, p.Rounds[1].ID)
	seen := map[string]bool{p.ID: true}
	for _, r := range p.Rounds {
		require.False(t, seen[r.ID])
		seen[r.ID] = true
		for _, th := range r.Themes {
			require.NotEmpty(t, th.ID)
			require.False(t, seen[th.ID])
			seen[th.ID] = true
			for _, qq := range th.Questions {
				require.NotEmpty(t, qq.ID)
				require.False(t, seen[qq.ID])
				seen[qq.ID] = true
			}
		}
	}
	ResetIDs(p)
	require.Empty(t, p.ID)
	require.Empty(t, p.Rounds[0].ID)
	require.Empty(t, p.Rounds[0].Themes[0].ID)
	require.Empty(t, p.Rounds[0].Themes[0].Questions[0].ID)

	id := NewID()
	require.Len(t, id, 36)
	require.Equal(t, byte('7'), id[14], "UUID version nibble must be 7")
}

func TestSummarize(t *testing.T) {
	p := samplePack()
	p.ID = "id1"
	p.Version = 2
	p.LogoMediaID = mediaC
	p.CreatedAt, p.UpdatedAt = 10, 20
	s := Summarize(p)
	require.Equal(t, Summary{
		ID: "id1", Version: 2, Name: "Sample pack", Language: "ru-RU", Difficulty: 3, Restriction: "12+",
		Tags: []string{"history", "Science"}, Authors: []string{"Alice", "Bob"},
		RoundCount: 2, ThemeCount: 2, QuestionCount: 5, HasMedia: true, LogoMediaID: mediaC,
		CreatedAt: 10, UpdatedAt: 20,
	}, s)

	// Text-only pack has no media; nil lists become empty ones.
	plain := &Pack{Name: "plain", Rounds: []Round{{Themes: []Theme{{Questions: []Question{
		{Price: 100, Params: QuestionParams{Question: []ContentItem{{Type: ContentText, Text: "t"}}}},
	}}}}}}
	ps := Summarize(plain)
	require.False(t, ps.HasMedia)
	require.Equal(t, 1, ps.QuestionCount)
	require.NotNil(t, ps.Tags)
	require.NotNil(t, ps.Authors)
}
