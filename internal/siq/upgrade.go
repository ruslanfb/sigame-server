package siq

import (
	"regexp"
	"strconv"
	"strings"

	"sigame/internal/packs"
)

// costRangeRe matches the v4 bagcat cost syntax "[a;b]" and "[a;b]/h".
var costRangeRe = regexp.MustCompile(`^\[\s*(\d+)\s*;\s*(\d+)\s*\](?:\s*/\s*(\d+))?$`)

// parseV4Cost parses a v4 "cost" value: N (fixed), 0 (round min/max),
// [a;b] (two extremes) or [a;b]/h (a..b step h).
func parseV4Cost(s string) (xmlNumberSet, bool) {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil && n >= 0 {
		v := strconv.Itoa(n)
		return xmlNumberSet{Minimum: v, Maximum: v, Step: "0"}, true
	}
	if m := costRangeRe.FindStringSubmatch(s); m != nil {
		step := "0"
		if m[3] != "" {
			step = m[3]
		}
		return xmlNumberSet{Minimum: m[1], Maximum: m[2], Step: step}, true
	}
	return xmlNumberSet{}, false
}

// legacyTypeResult is the v5 view of a v4 <type> element.
type legacyTypeResult struct {
	typeName        string     // v5 type name ("" = simple)
	params          []xmlParam // theme / price / selectionMode or custom simple params
	discardScenario bool       // secretNoQuestion has no content
	costInvalid     string     // non-empty when the cost could not be parsed (the raw value)
}

// upgradeLegacyType applies the type mapping of SIPackages Question.Upgrade:
// simple→"", auction→stake, sponsored→noRisk, cat→secret, bagcat→secret |
// secretPublicPrice | secretNoQuestion by "knows"; cost→price numberSet,
// self→selectionMode. Unknown names are kept as custom types and their
// parameters become simple params. nominalPrice is the fallback for an
// unparsable cost.
func upgradeLegacyType(typeName string, tparams []xmlSimpleParam, nominalPrice int) legacyTypeResult {
	get := func(name string) (string, bool) {
		for _, p := range tparams {
			if strings.EqualFold(strings.TrimSpace(p.Name), name) {
				return p.Value, true
			}
		}
		return "", false
	}
	res := legacyTypeResult{}
	switch strings.ToLower(strings.TrimSpace(typeName)) {
	case "", "simple", "withbutton":
		res.typeName = ""
	case "auction":
		res.typeName = string(packs.QStake)
	case "sponsored", "foryourself":
		res.typeName = string(packs.QNoRisk)
	case "cat", "bagcat":
		knows := "after"
		self := false
		if strings.EqualFold(strings.TrimSpace(typeName), "bagcat") {
			if v, ok := get("knows"); ok {
				knows = strings.ToLower(strings.TrimSpace(v))
			}
			if v, ok := get("self"); ok {
				self = strings.EqualFold(strings.TrimSpace(v), "true")
			}
		}
		switch knows {
		case "before":
			res.typeName = string(packs.QSecretPublicPrice)
		case "never":
			res.typeName = string(packs.QSecretNoQuestion)
			res.discardScenario = true
		default:
			res.typeName = string(packs.QSecret)
		}
		if res.typeName != string(packs.QSecretNoQuestion) {
			if theme, ok := get("theme"); ok {
				res.params = append(res.params, xmlParam{Name: "theme", Text: theme})
			}
		}
		cost, hasCost := get("cost")
		ns, ok := parseV4Cost(cost)
		if !ok {
			if hasCost {
				res.costInvalid = cost
			}
			v := strconv.Itoa(max(nominalPrice, 0))
			ns = xmlNumberSet{Minimum: v, Maximum: v, Step: "0"}
		}
		res.params = append(res.params, xmlParam{Name: "price", Type: "numberSet", NumberSet: &ns})
		mode := string(packs.SelectExceptCurrent)
		if self {
			mode = string(packs.SelectAny)
		}
		res.params = append(res.params, xmlParam{Name: "selectionMode", Text: mode})
	default:
		res.typeName = strings.TrimSpace(typeName)
		for _, p := range tparams {
			res.params = append(res.params, xmlParam{Name: strings.TrimSpace(p.Name), Text: p.Value})
		}
	}
	return res
}

// atomsToItems converts a v4 scenario into v5 question/answer content items.
// Atoms after the first marker become the answer. say→text+replic,
// voice→audio+background, "@name"→isRef, time=-1→waitForFinish=False,
// time=N→duration N seconds (kept in milliseconds).
func atomsToItems(atoms []xmlAtom) (question, answer []xmlItem) {
	afterMarker := false
	for _, a := range atoms {
		t := strings.ToLower(strings.TrimSpace(a.Type))
		if t == "marker" {
			afterMarker = true
			continue
		}
		it := xmlItem{Text: a.Text}
		switch t {
		case "", "text":
		case "say":
			it.Placement = string(packs.PlaceReplic)
		case "voice", "audio":
			it.Type = string(packs.ContentAudio)
			it.Placement = string(packs.PlaceBackground)
		case "image", "video", "html":
			it.Type = t
		default:
			it.Type = t // reported as unknown by the item mapper
		}
		if it.Type == "image" || it.Type == "audio" || it.Type == "video" || it.Type == "html" {
			if strings.HasPrefix(it.Text, "@") {
				it.IsRef = "True"
				it.Text = it.Text[1:]
			}
		}
		if tm := strings.TrimSpace(a.Time); tm != "" {
			if f, err := strconv.ParseFloat(tm, 64); err == nil {
				switch {
				case f < 0:
					it.WaitForFinish = "False"
				case f > 0:
					it.durationMs = int64(f*1000 + 0.5)
				}
			}
		}
		if afterMarker {
			answer = append(answer, it)
		} else {
			question = append(question, it)
		}
	}
	return question, answer
}

// upgradeQuestion returns a v5 view of a legacy question: the type attribute
// holds the mapped type name and Params holds the synthesised parameters in
// the order theme, price, selectionMode, question, answer, custom params.
// The returned copy has TypeElem and Scenario cleared.
func upgradeQuestion(xq *xmlQuestion, price int) (*xmlQuestion, legacyTypeResult) {
	out := *xq
	out.TypeElem = nil
	out.Scenario = nil
	out.Params = &xmlParams{Params: []xmlParam{}}
	if price == -1 {
		out.Type = ""
		return &out, legacyTypeResult{}
	}
	typeName := xq.Type
	var tparams []xmlSimpleParam
	if xq.TypeElem != nil {
		typeName = xq.TypeElem.Name
		tparams = xq.TypeElem.Params
	}
	res := upgradeLegacyType(typeName, tparams, price)
	out.Type = res.typeName
	var params []xmlParam
	var custom []xmlParam
	for _, p := range res.params {
		switch p.Name {
		case "theme", "price", "selectionMode":
			params = append(params, p)
		default:
			custom = append(custom, p)
		}
	}
	if !res.discardScenario && xq.Scenario != nil {
		q, a := atomsToItems(xq.Scenario.Atoms)
		if len(q) > 0 {
			params = append(params, xmlParam{Name: "question", Type: "content", Items: q})
		}
		if len(a) > 0 {
			params = append(params, xmlParam{Name: "answer", Type: "content", Items: a})
		}
	}
	params = append(params, custom...)
	out.Params.Params = params
	return &out, res
}
