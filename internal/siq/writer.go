package siq

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"sigame/internal/media"
	"sigame/internal/packs"
)

// ContentTypesXML is the Open-Packaging-Conventions leftover that old clients
// expect; it is byte-identical to the file written by SIQuester.
const ContentTypesXML = "\xEF\xBB\xBF" + xmlDeclaration +
	`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="si/xml" /></Types>`

// defaultModTime is used for zip entries when the pack has no UpdatedAt.
var defaultModTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// Export writes p as a SIQ version 5 archive: content.xml (deflate, UTF-8
// BOM, siq_5 namespace), [Content_Types].xml and every referenced media file
// stored uncompressed under Images/Audio/Video/Html, streamed from store.
// Entry order and names are deterministic for a given pack and store.
func Export(ctx context.Context, p *packs.Pack, store media.Store, w io.Writer) error {
	ex := &exporter{ctx: ctx, store: store, names: map[string]string{}, used: map[string]string{}, metas: map[string]media.Meta{}}
	if err := ex.resolveMedia(p); err != nil {
		return err
	}
	xp, err := ex.buildPackage(p)
	if err != nil {
		return err
	}
	modTime := defaultModTime
	if p.UpdatedAt > 0 {
		modTime = time.UnixMilli(p.UpdatedAt).UTC()
	}
	zw := zip.NewWriter(w)
	cw, err := zw.CreateHeader(&zip.FileHeader{Name: "content.xml", Method: zip.Deflate, Modified: modTime})
	if err != nil {
		return fmt.Errorf("siq export: %w", err)
	}
	if err := encodeContentXML(cw, xp); err != nil {
		return fmt.Errorf("siq export: %w", err)
	}
	tw, err := zw.CreateHeader(&zip.FileHeader{Name: "[Content_Types].xml", Method: zip.Deflate, Modified: modTime})
	if err != nil {
		return fmt.Errorf("siq export: %w", err)
	}
	if _, err := io.WriteString(tw, ContentTypesXML); err != nil {
		return fmt.Errorf("siq export: %w", err)
	}
	for _, id := range ex.sortedIDs() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := ex.writeMedia(zw, id, modTime); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("siq export: %w", err)
	}
	return nil
}

type exporter struct {
	ctx   context.Context
	store media.Store
	names map[string]string     // media ID → "Folder/name.ext"
	used  map[string]string     // lower-cased entry name → media ID
	metas map[string]media.Meta // media ID → Meta
}

// resolveMedia assigns an entry name to every media ID of the pack, in the
// pack's traversal order, so that de-duplication suffixes are stable.
func (ex *exporter) resolveMedia(p *packs.Pack) error {
	if p.LogoMediaID != "" {
		if _, err := ex.entryName(p.LogoMediaID, packs.ContentImage); err != nil {
			return err
		}
	}
	var walkItems func(items []packs.ContentItem) error
	walkItems = func(items []packs.ContentItem) error {
		for _, it := range items {
			if it.MediaID == "" {
				continue
			}
			if _, err := ex.entryName(it.MediaID, it.Type); err != nil {
				return err
			}
		}
		return nil
	}
	var walkParams func(params []packs.Param) error
	walkParams = func(params []packs.Param) error {
		for _, pr := range params {
			if err := walkItems(pr.Items); err != nil {
				return err
			}
			if err := walkParams(pr.Params); err != nil {
				return err
			}
		}
		return nil
	}
	for _, r := range p.Rounds {
		for _, t := range r.Themes {
			for _, q := range t.Questions {
				if err := walkItems(q.Params.Question); err != nil {
					return err
				}
				if err := walkItems(q.Params.Answer); err != nil {
					return err
				}
				for _, o := range q.Params.AnswerOptions {
					if err := walkItems(o.Content); err != nil {
						return err
					}
				}
				if err := walkParams(q.Extra); err != nil {
					return err
				}
				for _, s := range q.Script {
					if err := walkParams(s.Params); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// entryName returns the zip entry name for a media ID, allocating one on
// first use: <Folder>/<sanitized original name with the store's extension>,
// de-duplicated with -2, -3, ... suffixes.
func (ex *exporter) entryName(id string, ct packs.ContentType) (string, error) {
	if n, ok := ex.names[id]; ok {
		return n, nil
	}
	meta, err := ex.store.Stat(ex.ctx, id)
	if err != nil {
		if errors.Is(err, media.ErrNotFound) {
			return "", fmt.Errorf("%w: %s", ErrMediaNotFound, id)
		}
		return "", fmt.Errorf("siq export: stat media %s: %w", id, err)
	}
	folder := folderForKind(string(meta.Kind))
	if folder == "" {
		folder = folderForKind(string(ct))
	}
	if folder == "" {
		folder = folderImages
	}
	base := sanitizeEntryName(meta.OriginalName)
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(base), "."))
	stem := strings.TrimSuffix(base, path.Ext(base))
	if stem == "" {
		stem = id
		if len(stem) > 16 {
			stem = stem[:16]
		}
	}
	if want := strings.ToLower(strings.TrimPrefix(meta.Ext, ".")); want != "" && !sameExt(ext, want) {
		ext = want
	}
	name := stem
	if ext != "" {
		name += "." + ext
	}
	full := folder + "/" + name
	for n := 2; ; n++ {
		owner, taken := ex.used[strings.ToLower(full)]
		if !taken || owner == id {
			break
		}
		name = stem + "-" + strconv.Itoa(n)
		if ext != "" {
			name += "." + ext
		}
		full = folder + "/" + name
	}
	ex.names[id] = full
	ex.used[strings.ToLower(full)] = id
	ex.metas[id] = meta
	return full, nil
}

func sameExt(a, b string) bool {
	if a == b {
		return true
	}
	alias := map[string]string{"jpg": "jpeg", "htm": "html", "tif": "tiff"}
	if v, ok := alias[a]; ok {
		a = v
	}
	if v, ok := alias[b]; ok {
		b = v
	}
	return a == b
}

func (ex *exporter) sortedIDs() []string {
	ids := make([]string, 0, len(ex.names))
	for id := range ex.names {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ex.names[ids[i]] < ex.names[ids[j]] })
	return ids
}

func (ex *exporter) writeMedia(zw *zip.Writer, id string, modTime time.Time) error {
	rc, _, err := ex.store.Open(ex.ctx, id)
	if err != nil {
		if errors.Is(err, media.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrMediaNotFound, id)
		}
		return fmt.Errorf("siq export: open media %s: %w", id, err)
	}
	defer rc.Close()
	hw, err := zw.CreateHeader(&zip.FileHeader{Name: ex.names[id], Method: zip.Store, Modified: modTime})
	if err != nil {
		return fmt.Errorf("siq export: %w", err)
	}
	if _, err := io.Copy(hw, rc); err != nil {
		return fmt.Errorf("siq export: copy media %s: %w", id, err)
	}
	return nil
}

// buildPackage converts the pack into the XML model.
func (ex *exporter) buildPackage(p *packs.Pack) (*xmlPackage, error) {
	xp := &xmlPackage{
		Name:        p.Name,
		Version:     "5",
		ID:          p.SIQID,
		Restriction: p.Restriction,
		Date:        p.Date,
		Publisher:   p.Publisher,
		ContactURI:  p.ContactURI,
		Language:    p.Language,
		Tags:        p.Tags,
		Info:        infoXML(p.Info),
		Rounds:      []xmlRound{},
	}
	if xp.ID == "" {
		xp.ID = p.ID
	}
	if xp.ID == "" {
		xp.ID = uuid.Must(uuid.NewV7()).String()
	}
	if p.Difficulty > 0 {
		xp.Difficulty = strconv.Itoa(p.Difficulty)
	}
	switch {
	case p.LogoMediaID != "":
		xp.Logo = "@" + path.Base(ex.names[p.LogoMediaID])
	case p.LogoURL != "":
		xp.Logo = p.LogoURL
	}
	if p.Extra != nil && (len(p.Extra.GlobalAuthors) > 0 || len(p.Extra.GlobalSources) > 0) {
		g := &xmlGlobal{}
		for _, a := range p.Extra.GlobalAuthors {
			g.Authors = append(g.Authors, xmlAuthor{ID: a.ID, Name: a.Name, SecondName: a.SecondName, Surname: a.Surname, Country: a.Country, City: a.City})
		}
		for _, s := range p.Extra.GlobalSources {
			g.Sources = append(g.Sources, xmlSource{ID: s.ID, Author: s.Author, Title: s.Title, Year: s.Year, Publish: s.Publish, City: s.City})
		}
		xp.Global = g
	}
	for _, id := range ex.sortedIDs() {
		xp.Files = append(xp.Files, xmlFile{Name: ex.names[id], Hash: strings.ToUpper(ex.metas[id].ID)})
	}
	for _, r := range p.Rounds {
		xr := xmlRound{Name: r.Name, Info: infoXML(r.Info), Themes: []xmlTheme{}}
		if r.Type == packs.RoundFinal {
			xr.Type = "final"
		}
		for _, t := range r.Themes {
			xt := xmlTheme{Name: t.Name, Info: infoXML(t.Info), Questions: []xmlQuestion{}}
			for _, q := range t.Questions {
				xq, err := ex.question(q)
				if err != nil {
					return nil, err
				}
				xt.Questions = append(xt.Questions, xq)
			}
			xr.Themes = append(xr.Themes, xt)
		}
		xp.Rounds = append(xp.Rounds, xr)
	}
	return xp, nil
}

func infoXML(i packs.Info) *xmlInfo {
	if len(i.Authors) == 0 && len(i.Sources) == 0 && i.Comments == "" && i.ShowmanComments == "" {
		return nil
	}
	return &xmlInfo{Authors: i.Authors, Sources: i.Sources, Comments: i.Comments, ShowmanComments: i.ShowmanComments}
}

func (ex *exporter) question(q packs.Question) (xmlQuestion, error) {
	xq := xmlQuestion{
		Price:  strconv.Itoa(q.Price),
		Info:   infoXML(q.Info),
		Params: &xmlParams{Params: []xmlParam{}},
		Right:  &xmlAnswers{Answers: q.Right},
	}
	if t := q.Type; t != "" && t != packs.QSimple {
		xq.Type = string(t)
	}
	if len(xq.Right.Answers) == 0 {
		xq.Right.Answers = []string{""}
	}
	if len(q.Wrong) > 0 {
		xq.Wrong = &xmlAnswers{Answers: q.Wrong}
	}
	if q.IsEmpty() {
		return xq, nil
	}
	pp := q.Params
	add := func(p xmlParam) { xq.Params.Params = append(xq.Params.Params, p) }
	if pp.Theme != "" {
		add(xmlParam{Name: "theme", Text: pp.Theme})
	}
	if pp.Price != nil {
		add(xmlParam{Name: "price", Type: "numberSet", NumberSet: numberSetXML(*pp.Price)})
	}
	if pp.SelectionMode != "" {
		add(xmlParam{Name: "selectionMode", Text: string(pp.SelectionMode)})
	}
	if len(pp.Question) > 0 {
		items, err := ex.items(pp.Question)
		if err != nil {
			return xq, err
		}
		add(xmlParam{Name: "question", Type: "content", Items: items})
	}
	if len(pp.Answer) > 0 {
		items, err := ex.items(pp.Answer)
		if err != nil {
			return xq, err
		}
		add(xmlParam{Name: "answer", Type: "content", Items: items})
	}
	if pp.AnswerType != "" && pp.AnswerType != packs.AnswerText {
		add(xmlParam{Name: "answerType", Text: string(pp.AnswerType)})
	}
	if len(pp.AnswerOptions) > 0 {
		group := xmlParam{Name: "answerOptions", Type: "group"}
		for _, o := range pp.AnswerOptions {
			items, err := ex.items(o.Content)
			if err != nil {
				return xq, err
			}
			group.Params = append(group.Params, xmlParam{Name: o.Label, Type: "content", Items: items})
		}
		add(group)
	}
	if pp.AnswerDeviation != 0 {
		add(xmlParam{Name: "answerDeviation", Text: strconv.FormatFloat(pp.AnswerDeviation, 'f', -1, 64)})
	}
	if pp.AnswerDurationMs > 0 {
		add(xmlParam{Name: "answerDuration", Text: strconv.FormatInt((pp.AnswerDurationMs+500)/1000, 10)})
	}
	for _, e := range q.Extra {
		xpr, err := ex.param(e)
		if err != nil {
			return xq, err
		}
		add(xpr)
	}
	if len(q.Script) > 0 {
		xq.Script = &xmlScript{}
		for _, s := range q.Script {
			step := xmlStep{Type: s.Type}
			for _, pr := range s.Params {
				xpr, err := ex.param(pr)
				if err != nil {
					return xq, err
				}
				step.Params = append(step.Params, xpr)
			}
			xq.Script.Steps = append(xq.Script.Steps, step)
		}
	}
	return xq, nil
}

func numberSetXML(ns packs.NumberSet) *xmlNumberSet {
	return &xmlNumberSet{Minimum: strconv.Itoa(ns.Min), Maximum: strconv.Itoa(ns.Max), Step: strconv.Itoa(ns.Step)}
}

func (ex *exporter) param(p packs.Param) (xmlParam, error) {
	x := xmlParam{Name: p.Name, Type: p.Type}
	if p.IsRef {
		x.IsRef = "True"
	}
	switch strings.ToLower(p.Type) {
	case "content":
		items, err := ex.items(p.Items)
		if err != nil {
			return x, err
		}
		x.Items = items
	case "group":
		for _, sub := range p.Params {
			xs, err := ex.param(sub)
			if err != nil {
				return x, err
			}
			x.Params = append(x.Params, xs)
		}
	case "numberset":
		if p.NumberSet != nil {
			x.NumberSet = numberSetXML(*p.NumberSet)
		}
	default:
		x.Text = p.Value
		if len(p.Items) > 0 {
			items, err := ex.items(p.Items)
			if err != nil {
				return x, err
			}
			x.Items = items
		}
	}
	return x, nil
}

func (ex *exporter) items(items []packs.ContentItem) ([]xmlItem, error) {
	out := make([]xmlItem, 0, len(items))
	for _, it := range items {
		x, err := ex.item(it)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, nil
}

// item writes one content item following the SIPackages writer rules: type
// omitted for text, isRef="True" for stored media, placement only when it
// differs from the default (audio: always "background"), duration as
// hh:mm:ss when > 0, waitForFinish="False" when NoWait.
func (ex *exporter) item(it packs.ContentItem) (xmlItem, error) {
	x := xmlItem{}
	ct := it.Type
	if ct == "" {
		ct = packs.ContentText
	}
	if ct != packs.ContentText {
		x.Type = string(ct)
	}
	switch {
	case it.MediaID != "":
		name, err := ex.entryName(it.MediaID, ct)
		if err != nil {
			return x, err
		}
		x.IsRef = "True"
		x.Text = path.Base(name)
	case it.URL != "":
		x.Text = it.URL
	default:
		x.Text = it.Text
	}
	if ct == packs.ContentAudio {
		pl := it.Placement
		if pl == "" {
			pl = packs.PlaceBackground
		}
		x.Placement = string(pl)
	} else if it.Placement != "" && it.Placement != packs.PlaceScreen {
		x.Placement = string(it.Placement)
	}
	if it.DurationMs > 0 {
		if d := formatDuration(it.DurationMs); d != "" {
			x.Duration = d
		}
	}
	if it.NoWait {
		x.WaitForFinish = "False"
	}
	return x, nil
}

// formatDuration renders milliseconds as hh:mm:ss (rounded to whole
// seconds); "" when it rounds to zero.
func formatDuration(ms int64) string {
	sec := (ms + 500) / 1000
	if sec <= 0 {
		return ""
	}
	h, m, s := sec/3600, (sec%3600)/60, sec%60
	if h > 99 {
		h = 99
	}
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}
