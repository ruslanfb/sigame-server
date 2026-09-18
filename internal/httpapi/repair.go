package httpapi

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"sigame/internal/packs"
	"sigame/internal/siq"
)

// CodeAutoFixed is the report entry code of a mechanical repair applied to an
// imported pack so that it passes packs.Validate. The entry's path points at
// the repaired value and its message explains the change.
const CodeAutoFixed = "autoFixed"

// questionPath matches the JSON pointer prefix of a question.
var questionPath = regexp.MustCompile(`^/rounds/(\d+)/themes/(\d+)/questions/(\d+)(/.*)?$`)

// repairImportedPack normalises p, validates it and repairs the problems the
// .siq format tolerates but packs.Validate rejects, recording each change in
// rep. The repairs preserve play semantics:
//
//   - a numberSet whose step does not divide max-min (SIGame offers
//     min, min+step, ... <= max) gets max lowered to the last reachable value;
//   - a question without any content (an untouched placeholder in SIQuester)
//     becomes an empty slot (price -1).
//
// Anything else is returned as the remaining *packs.ValidationError. The
// logic lives here rather than in the siq package only because that package
// is owned elsewhere; it is the natural home once importer and validator
// converge.
func repairImportedPack(p *packs.Pack, rep *siq.Report) error {
	packs.Normalize(p)
	for attempt := 0; attempt < 2; attempt++ {
		err := packs.Validate(p)
		var ve *packs.ValidationError
		if !errors.As(err, &ve) {
			return err
		}
		fixed := false
		for _, pr := range ve.Problems {
			change, ok := fixProblem(p, pr)
			if !ok {
				continue
			}
			fixed = true
			rep.Entries = append(rep.Entries, siq.Entry{
				Level:   siq.LevelError,
				Code:    CodeAutoFixed,
				Path:    pr.Path,
				Message: pr.Message + "; " + change,
			})
		}
		if !fixed {
			return ve
		}
		packs.Normalize(p)
	}
	return packs.Validate(p)
}

// fixProblem applies one repair and describes it; ok is false when the
// problem is not one of the known-safe cases.
func fixProblem(p *packs.Pack, pr packs.Problem) (change string, ok bool) {
	m := questionPath.FindStringSubmatch(pr.Path)
	if m == nil {
		return "", false
	}
	i, _ := strconv.Atoi(m[1])
	j, _ := strconv.Atoi(m[2])
	k, _ := strconv.Atoi(m[3])
	if i >= len(p.Rounds) || j >= len(p.Rounds[i].Themes) || k >= len(p.Rounds[i].Themes[j].Questions) {
		return "", false
	}
	q := &p.Rounds[i].Themes[j].Questions[k]
	switch {
	case m[4] == "/params/price/step" && pr.Code == packs.CodeMismatch:
		ns := q.Params.Price
		if ns == nil || ns.Step <= 0 || ns.Max <= ns.Min {
			return "", false
		}
		newMax := ns.Min + (ns.Max-ns.Min)/ns.Step*ns.Step
		if newMax == ns.Max {
			return "", false
		}
		ns.Max = newMax
		return fmt.Sprintf("price maximum lowered to %d (the selectable values are unchanged)", newMax), true
	case m[4] == "/params/question" && pr.Code == packs.CodeTooFew:
		if len(q.Params.Question) > 0 || q.IsEmpty() {
			return "", false
		}
		q.Price = -1
		return "question has no content and was turned into an empty slot (price -1)", true
	}
	return "", false
}
