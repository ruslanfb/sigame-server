package packs

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Additional limits not covered by the model constants.
const (
	MaxShortLen     = 16   // language, restriction
	MaxDateLen      = 64   // free-form date
	MaxLabelLen     = 8    // select option label
	MaxDifficulty   = 10   // difficulty scale 0..10
	MaxAnswerLen    = 1500 // one Right/Wrong entry
	SHA256HexLength = 64   // media id length
)

// Validate checks structural and semantic rules of a pack and returns a
// *ValidationError listing every problem, or nil when the pack is valid.
// It is pure: media existence is checked by the repository (see Repo.Create),
// not here. Validate is normally run after Normalize; when it is not, an
// empty round type is read as "standard" and an empty question type as
// "simple", mirroring Question.EffectiveType.
func Validate(p *Pack) error {
	v := &ValidationError{}
	if p == nil {
		v.add("", CodeRequired, "pack is required")
		return v
	}
	v.checkStr("/name", p.Name, true, MaxTextLen)
	v.checkStr("/language", p.Language, false, MaxShortLen)
	v.checkStr("/restriction", p.Restriction, false, MaxShortLen)
	v.checkStr("/date", p.Date, false, MaxDateLen)
	v.checkStr("/publisher", p.Publisher, false, MaxTextLen)
	v.checkStr("/contactUri", p.ContactURI, false, MaxContentValueLen)
	if p.Difficulty < 0 || p.Difficulty > MaxDifficulty {
		v.add("/difficulty", CodeOutOfRange, "difficulty must be between 0 and %d", MaxDifficulty)
	}
	if p.LogoMediaID != "" && !isSHA256Hex(p.LogoMediaID) {
		v.add("/logoMediaId", CodeInvalidValue, "media id must be 64 lowercase hex characters (sha256)")
	}
	if p.LogoURL != "" {
		v.checkURL("/logoUrl", p.LogoURL)
		if p.LogoMediaID != "" {
			v.add("/logoUrl", CodeUnexpected, "set either logoMediaId or logoUrl, not both")
		}
	}
	v.checkStrList("/tags", p.Tags, MaxListItems, MaxTextLen)
	v.checkInfo("/info", &p.Info)
	v.checkCount("/rounds", len(p.Rounds), MaxRounds)
	for i := range p.Rounds {
		v.checkRound(fmt.Sprintf("/rounds/%d", i), &p.Rounds[i])
	}
	return v.orNil()
}

func (v *ValidationError) checkRound(path string, r *Round) {
	v.checkStr(path+"/name", r.Name, false, MaxTextLen)
	final := false
	switch r.Type {
	case "", RoundStandard:
	case RoundFinal:
		final = true
	default:
		v.add(path+"/type", CodeInvalidValue, "round type must be %q or %q", RoundStandard, RoundFinal)
	}
	v.checkInfo(path+"/info", &r.Info)
	v.checkCount(path+"/themes", len(r.Themes), MaxThemesPerRound)
	for i := range r.Themes {
		tp := fmt.Sprintf("%s/themes/%d", path, i)
		t := &r.Themes[i]
		v.checkStr(tp+"/name", t.Name, false, MaxTextLen)
		v.checkInfo(tp+"/info", &t.Info)
		v.checkCount(tp+"/questions", len(t.Questions), MaxQuestionsPerTheme)
		if final && len(t.Questions) == 0 {
			v.add(tp+"/questions", CodeTooFew, "every theme of a final round needs at least one question")
		}
		for j := range t.Questions {
			v.checkQuestion(fmt.Sprintf("%s/questions/%d", tp, j), &t.Questions[j])
		}
	}
}

func (v *ValidationError) checkQuestion(path string, q *Question) {
	if q.Price < -1 {
		v.add(path+"/price", CodeOutOfRange, "price must be >= -1 (-1 marks an empty slot)")
	}
	v.checkStr(path+"/type", string(q.Type), false, MaxTextLen)
	typ := q.EffectiveType()
	v.checkInfo(path+"/info", &q.Info)

	pp := path + "/params"
	answerless := q.IsEmpty() || typ == QSecretNoQuestion

	// Content.
	v.checkCount(pp+"/question", len(q.Params.Question), MaxItemsPerParam)
	if len(q.Params.Question) == 0 && !answerless {
		v.add(pp+"/question", CodeTooFew, "question needs at least one content item")
	}
	v.checkItems(pp+"/question", q.Params.Question)
	v.checkCount(pp+"/answer", len(q.Params.Answer), MaxItemsPerParam)
	v.checkItems(pp+"/answer", q.Params.Answer)
	v.checkStr(pp+"/theme", q.Params.Theme, false, MaxTextLen)

	// Secret-question parameters: validated whenever present; extra
	// parameters on other question types are ignored, invalid values are not.
	if q.Params.Price != nil {
		v.checkNumberSet(pp+"/price", q.Params.Price)
	}
	switch q.Params.SelectionMode {
	case "", SelectAny, SelectExceptCurrent:
	default:
		v.add(pp+"/selectionMode", CodeInvalidValue, "selectionMode must be empty, %q or %q", SelectAny, SelectExceptCurrent)
	}

	// Answer.
	switch q.Params.AnswerType {
	case "", AnswerText, AnswerSelect, AnswerNumber, AnswerPoint, AnswerClient:
	default:
		v.add(pp+"/answerType", CodeInvalidValue, "answerType must be one of text, select, number, point, client")
	}
	if q.Params.AnswerDeviation < 0 {
		v.add(pp+"/answerDeviation", CodeOutOfRange, "answerDeviation must be >= 0")
	}
	if q.Params.AnswerDurationMs < 0 {
		v.add(pp+"/answerDurationMs", CodeOutOfRange, "answerDurationMs must be >= 0")
	}
	labels := map[string]bool{}
	v.checkCount(pp+"/answerOptions", len(q.Params.AnswerOptions), MaxAnswers)
	for i := range q.Params.AnswerOptions {
		op := fmt.Sprintf("%s/answerOptions/%d", pp, i)
		o := &q.Params.AnswerOptions[i]
		label := strings.ToUpper(strings.TrimSpace(o.Label))
		if label == "" {
			v.add(op+"/label", CodeRequired, "option label is required")
		} else if utf8.RuneCountInString(label) > MaxLabelLen {
			v.add(op+"/label", CodeTooLong, "option label must be at most %d characters", MaxLabelLen)
		} else if labels[label] {
			v.add(op+"/label", CodeDuplicate, "option label %q is used twice", label)
		}
		labels[label] = true
		v.checkCount(op+"/content", len(o.Content), MaxItemsPerParam)
		v.checkItems(op+"/content", o.Content)
	}

	v.checkCount(path+"/right", len(q.Right), MaxAnswers)
	for i, s := range q.Right {
		v.checkStr(fmt.Sprintf("%s/right/%d", path, i), s, false, MaxAnswerLen)
	}
	v.checkCount(path+"/wrong", len(q.Wrong), MaxAnswers)
	for i, s := range q.Wrong {
		v.checkStr(fmt.Sprintf("%s/wrong/%d", path, i), s, false, MaxAnswerLen)
	}
	if !answerless {
		if len(q.Right) == 0 {
			v.add(path+"/right", CodeTooFew, "at least one right answer entry is required (it may be empty)")
		} else {
			switch q.Params.AnswerType {
			case AnswerSelect:
				if len(q.Params.AnswerOptions) < 2 {
					v.add(pp+"/answerOptions", CodeTooFew, "select questions need at least two options")
				}
				if !labels[strings.ToUpper(strings.TrimSpace(q.Right[0]))] {
					v.add(path+"/right/0", CodeMismatch, "right answer %q is not one of the option labels", q.Right[0])
				}
			case AnswerNumber:
				if _, err := parseNumber(q.Right[0]); err != nil {
					v.add(path+"/right/0", CodeInvalidValue, "right answer %q of a number question must be a number", q.Right[0])
				}
			}
		}
	}

	// Custom scripts and preserved params.
	v.checkCount(path+"/script", len(q.Script), MaxScriptSteps)
	for i := range q.Script {
		sp := fmt.Sprintf("%s/script/%d", path, i)
		if strings.TrimSpace(q.Script[i].Type) == "" {
			v.add(sp+"/type", CodeRequired, "script step type is required")
		}
		v.checkParams(sp+"/params", q.Script[i].Params, 0)
	}
	v.checkParams(path+"/extraParams", q.Extra, 0)
}

// maxParamDepth bounds recursion into nested group params.
const maxParamDepth = 8

func (v *ValidationError) checkParams(path string, params []Param, depth int) {
	v.checkCount(path, len(params), MaxItemsPerParam)
	if depth > maxParamDepth {
		v.add(path, CodeInvalidValue, "params are nested too deeply")
		return
	}
	for i := range params {
		pp := fmt.Sprintf("%s/%d", path, i)
		pr := &params[i]
		if strings.TrimSpace(pr.Name) == "" {
			v.add(pp+"/name", CodeRequired, "param name is required")
		}
		v.checkCount(pp+"/items", len(pr.Items), MaxItemsPerParam)
		v.checkItems(pp+"/items", pr.Items)
		if pr.NumberSet != nil {
			v.checkNumberSet(pp+"/numberSet", pr.NumberSet)
		}
		v.checkParams(pp+"/params", pr.Params, depth+1)
	}
}

func (v *ValidationError) checkNumberSet(path string, ns *NumberSet) {
	if ns.Min < 0 {
		v.add(path+"/min", CodeOutOfRange, "min must be >= 0")
	}
	if ns.Max < 0 {
		v.add(path+"/max", CodeOutOfRange, "max must be >= 0")
	}
	if ns.Step < 0 {
		v.add(path+"/step", CodeOutOfRange, "step must be >= 0")
	}
	if ns.Min > ns.Max {
		v.add(path+"/min", CodeMismatch, "min (%d) must not exceed max (%d)", ns.Min, ns.Max)
	} else if ns.Step > 0 && (ns.Max-ns.Min)%ns.Step != 0 {
		v.add(path+"/step", CodeMismatch, "max-min (%d) must be a multiple of step (%d)", ns.Max-ns.Min, ns.Step)
	}
}

func (v *ValidationError) checkItems(path string, items []ContentItem) {
	for i := range items {
		v.checkItem(fmt.Sprintf("%s/%d", path, i), &items[i])
	}
}

func (v *ValidationError) checkItem(path string, it *ContentItem) {
	switch it.Type {
	case ContentText:
		if strings.TrimSpace(it.Text) == "" {
			v.add(path+"/text", CodeRequired, "text item needs text")
		} else if utf8.RuneCountInString(it.Text) > MaxContentValueLen {
			v.add(path+"/text", CodeTooLong, "text must be at most %d characters", MaxContentValueLen)
		}
		if it.MediaID != "" {
			v.add(path+"/mediaId", CodeUnexpected, "text item must not reference media")
		}
		if it.URL != "" {
			v.add(path+"/url", CodeUnexpected, "text item must not reference a URL")
		}
	case ContentImage, ContentAudio, ContentVideo, ContentHTML:
		switch {
		case it.MediaID == "" && it.URL == "":
			v.add(path+"/mediaId", CodeRequired, "%s item needs mediaId or url", it.Type)
		case it.MediaID != "" && it.URL != "":
			v.add(path+"/url", CodeUnexpected, "set either mediaId or url, not both")
		}
		if it.MediaID != "" && !isSHA256Hex(it.MediaID) {
			v.add(path+"/mediaId", CodeInvalidValue, "media id must be 64 lowercase hex characters (sha256)")
		}
		if it.URL != "" {
			v.checkURL(path+"/url", it.URL)
		}
		if it.Text != "" {
			v.add(path+"/text", CodeUnexpected, "%s item must not carry text", it.Type)
		}
	default:
		v.add(path+"/type", CodeInvalidValue, "content type must be one of text, image, audio, video, html")
	}
	switch it.Placement {
	case "", PlaceScreen:
	case PlaceReplic:
		if it.Type != ContentText {
			v.add(path+"/placement", CodeMismatch, "replic placement is only valid for text items")
		}
	case PlaceBackground:
		if it.Type != ContentAudio {
			v.add(path+"/placement", CodeMismatch, "background placement is only valid for audio items")
		}
	default:
		v.add(path+"/placement", CodeInvalidValue, "placement must be empty, screen, replic or background")
	}
	if it.DurationMs < 0 {
		v.add(path+"/durationMs", CodeOutOfRange, "durationMs must be >= 0")
	}
}

func (v *ValidationError) checkInfo(path string, in *Info) {
	v.checkStrList(path+"/authors", in.Authors, MaxListItems, MaxTextLen)
	v.checkStrList(path+"/sources", in.Sources, MaxListItems, MaxTextLen)
	v.checkStr(path+"/comments", in.Comments, false, MaxContentValueLen)
	v.checkStr(path+"/showmanComments", in.ShowmanComments, false, MaxContentValueLen)
}

func (v *ValidationError) checkStrList(path string, list []string, maxItems, maxLen int) {
	v.checkCount(path, len(list), maxItems)
	for i, s := range list {
		v.checkStr(fmt.Sprintf("%s/%d", path, i), s, false, maxLen)
	}
}

func (v *ValidationError) checkStr(path, s string, required bool, maxLen int) {
	if required && strings.TrimSpace(s) == "" {
		v.add(path, CodeRequired, "value is required")
		return
	}
	if utf8.RuneCountInString(s) > maxLen {
		v.add(path, CodeTooLong, "value must be at most %d characters", maxLen)
	}
}

func (v *ValidationError) checkCount(path string, n, maxItems int) {
	if n > maxItems {
		v.add(path, CodeTooMany, "at most %d items are allowed", maxItems)
	}
}

func (v *ValidationError) checkURL(path, s string) {
	if utf8.RuneCountInString(s) > MaxContentValueLen {
		v.add(path, CodeTooLong, "url must be at most %d characters", MaxContentValueLen)
		return
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		v.add(path, CodeInvalidValue, "url must be an absolute http(s) URL")
	}
}

// isSHA256Hex reports whether s is a 64-character lowercase hex string.
func isSHA256Hex(s string) bool {
	if len(s) != SHA256HexLength {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// parseNumber parses a numeric answer, accepting a decimal comma.
func parseNumber(s string) (float64, error) {
	s = strings.TrimSpace(strings.Replace(s, ",", ".", 1))
	return strconv.ParseFloat(s, 64)
}
