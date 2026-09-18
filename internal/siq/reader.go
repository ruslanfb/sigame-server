package siq

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"sigame/internal/media"
	"sigame/internal/packs"
)

// Default import limits.
const (
	DefaultMaxEntries           = 5000
	DefaultMaxTotalUncompressed = int64(2) << 30 // 2 GiB
	DefaultMaxRatio             = int64(200)
	maxContentXMLSize           = int64(32) << 20 // 32 MiB
	// ratioCheckFloor is the uncompressed size above which the per-entry
	// compression ratio is enforced; tiny files compress arbitrarily well.
	ratioCheckFloor = uint64(1) << 20
)

// ImportOptions tunes Import. Zero values mean the defaults above.
type ImportOptions struct {
	MaxEntries           int   // maximum number of zip entries
	MaxTotalUncompressed int64 // maximum sum of declared uncompressed sizes
	MaxRatio             int64 // maximum uncompressed/compressed ratio per entry (above ratioCheckFloor)
	SkipMedia            bool  // resolve media references but do not store the files (MediaID stays empty)
}

func (o ImportOptions) withDefaults() ImportOptions {
	if o.MaxEntries <= 0 {
		o.MaxEntries = DefaultMaxEntries
	}
	if o.MaxTotalUncompressed <= 0 {
		o.MaxTotalUncompressed = DefaultMaxTotalUncompressed
	}
	if o.MaxRatio <= 0 {
		o.MaxRatio = DefaultMaxRatio
	}
	return o
}

// Import reads a .siq archive (v3/v4/v5) and converts it to the packs model.
// Media files referenced with isRef/"@" are streamed into store (unless
// opts.SkipMedia) and referenced by their media ID. Import never fails on a
// single bad media file: the item is dropped and an error entry is added to
// the report. It fails only on a broken archive, a missing or malformed
// content.xml, a violated safety limit, a cancelled context or a store
// infrastructure error.
//
// The returned pack has fresh UUIDv7 IDs, Version 0 and zero timestamps (the
// repository assigns them on save); SIQID keeps the original package id.
func Import(ctx context.Context, r io.ReaderAt, size int64, store media.Store, opts ImportOptions) (*packs.Pack, *Report, error) {
	opts = opts.withDefaults()
	zr, err := zip.NewReader(r, size)
	if err != nil && !(errors.Is(err, zip.ErrInsecurePath) && zr != nil) {
		return nil, nil, fmt.Errorf("%w: %v", ErrBadZip, err)
	}
	if err := checkArchive(zr, opts); err != nil {
		return nil, nil, err
	}
	contentFile, prefix := findContentXML(zr)
	if contentFile == nil {
		return nil, nil, ErrNoContent
	}
	if contentFile.UncompressedSize64 > uint64(maxContentXMLSize) {
		return nil, nil, fmt.Errorf("%w: content.xml exceeds %d bytes", ErrTooLarge, maxContentXMLSize)
	}
	data, err := readEntry(contentFile, maxContentXMLSize)
	if err != nil {
		return nil, nil, err
	}
	xp, err := decodeContentXML(data)
	if err != nil {
		return nil, nil, err
	}

	im := &importer{
		ctx:   ctx,
		store: store,
		opts:  opts,
		rep:   newReport(),
		index: newMediaIndex(),
		cache: map[*zip.File]*cachedMedia{},
	}
	im.indexEntries(zr, contentFile, prefix)
	im.detectVersion(xp)

	pack, err := im.mapPackage(xp)
	if err != nil {
		return nil, im.rep, err
	}
	return pack, im.rep, nil
}

// checkArchive enforces the zip safety limits before anything is read.
func checkArchive(zr *zip.Reader, opts ImportOptions) error {
	if len(zr.File) > opts.MaxEntries {
		return fmt.Errorf("%w: %d > %d", ErrTooManyEntries, len(zr.File), opts.MaxEntries)
	}
	var total uint64
	for _, f := range zr.File {
		if err := checkEntryName(f.Name); err != nil {
			return err
		}
		total += f.UncompressedSize64
		if total > uint64(opts.MaxTotalUncompressed) {
			return fmt.Errorf("%w: uncompressed size exceeds %d bytes", ErrTooLarge, opts.MaxTotalUncompressed)
		}
		if f.Method != zip.Store && f.CompressedSize64 > 0 && f.UncompressedSize64 > ratioCheckFloor &&
			f.UncompressedSize64/f.CompressedSize64 > uint64(opts.MaxRatio) {
			return fmt.Errorf("%w: entry %q", ErrSuspiciousRatio, f.Name)
		}
	}
	return nil
}

// checkEntryName rejects absolute paths, drive letters, NUL bytes and ".."
// segments (with either separator).
func checkEntryName(name string) error {
	n := strings.ReplaceAll(name, "\\", "/")
	if strings.ContainsRune(n, 0) || strings.HasPrefix(n, "/") || (len(n) >= 2 && n[1] == ':') {
		return fmt.Errorf("%w: %q", ErrUnsafeEntry, name)
	}
	for _, seg := range strings.Split(n, "/") {
		if seg == ".." {
			return fmt.Errorf("%w: %q", ErrUnsafeEntry, name)
		}
	}
	return nil
}

// normalizeEntryName unifies separators and strips a leading "./".
func normalizeEntryName(name string) string {
	n := strings.ReplaceAll(name, "\\", "/")
	for strings.HasPrefix(n, "./") {
		n = n[2:]
	}
	return n
}

// findContentXML locates content.xml at the root or inside exactly one
// leading folder. It returns the entry and the folder prefix ("" or "dir/").
func findContentXML(zr *zip.Reader) (*zip.File, string) {
	var nested *zip.File
	nestedPrefix := ""
	for _, f := range zr.File {
		n := normalizeEntryName(f.Name)
		if strings.HasSuffix(n, "/") {
			continue
		}
		dir, base := splitEntry(n)
		if !strings.EqualFold(base, "content.xml") {
			continue
		}
		if dir == "" {
			return f, ""
		}
		if !strings.Contains(dir, "/") && nested == nil {
			nested = f
			nestedPrefix = dir + "/"
		}
	}
	return nested, nestedPrefix
}

func splitEntry(n string) (dir, base string) {
	if i := strings.LastIndexByte(n, '/'); i >= 0 {
		return n[:i], n[i+1:]
	}
	return "", n
}

// limitedReader fails with ErrTooLarge once more than limit bytes are read.
type limitedReader struct {
	r     io.Reader
	left  int64
	limit int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.left <= 0 {
		return 0, fmt.Errorf("%w: entry exceeds %d bytes", ErrTooLarge, l.limit)
	}
	if int64(len(p)) > l.left {
		p = p[:l.left]
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	return n, err
}

// readEntry reads a whole entry with a hard size cap.
func readEntry(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: open %q: %v", ErrBadZip, f.Name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(&limitedReader{r: rc, left: limit + 1, limit: limit})
	if err != nil {
		if errors.Is(err, ErrTooLarge) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: read %q: %v", ErrBadZip, f.Name, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: entry %q exceeds %d bytes", ErrTooLarge, f.Name, limit)
	}
	return data, nil
}

// cachedMedia is the outcome of storing one zip entry (shared by every item
// that references the same file).
type cachedMedia struct {
	id   string
	code string // report code when the file could not be stored
	msg  string
}

type importer struct {
	ctx   context.Context
	store media.Store
	opts  ImportOptions
	rep   *Report
	index *mediaIndex
	cache map[*zip.File]*cachedMedia

	authorsFile *zip.File // v4 Texts/authors.xml
	sourcesFile *zip.File // v4 Texts/sources.xml
	legacy      bool      // package version < 5
	upgraded    int       // number of questions converted from the v4 model
}

// indexEntries builds the media index and remembers the legacy Texts files.
func (im *importer) indexEntries(zr *zip.Reader, content *zip.File, prefix string) {
	for _, f := range zr.File {
		if f == content {
			continue
		}
		n := normalizeEntryName(f.Name)
		if strings.HasSuffix(n, "/") || n == "" {
			continue
		}
		if prefix != "" {
			if !strings.HasPrefix(n, prefix) {
				continue
			}
			n = n[len(prefix):]
		}
		parts := strings.Split(n, "/")
		base := parts[len(parts)-1]
		if base == ".DS_Store" || strings.HasPrefix(base, "._") || parts[0] == "__MACOSX" {
			continue
		}
		nonUTF8 := f.NonUTF8 || f.Flags&0x800 == 0
		switch {
		case len(parts) == 1:
			switch strings.ToLower(base) {
			case "[content_types].xml", "quality.marker":
				continue
			}
			im.index.add("", base, nonUTF8, f)
		case len(parts) == 2 && strings.EqualFold(parts[0], "texts"):
			switch strings.ToLower(base) {
			case "authors.xml":
				im.authorsFile = f
			case "sources.xml":
				im.sourcesFile = f
			}
		case len(parts) == 2 && canonicalFolder(parts[0]) != "":
			im.index.add(canonicalFolder(parts[0]), base, nonUTF8, f)
		default:
			im.index.add("*", base, nonUTF8, f)
			im.rep.infof(CodeExtraFolderEntry, "", fmt.Sprintf("entry %q is outside the known folders", n))
		}
	}
}

// detectVersion reads the version attribute; a missing one is guessed from
// the presence of <scenario>.
func (im *importer) detectVersion(xp *xmlPackage) {
	ver := 0
	if v, err := strconv.ParseFloat(strings.TrimSpace(xp.Version), 64); err == nil && v > 0 {
		ver = int(v)
	}
	if ver == 0 {
		ver = 5
		for _, r := range xp.Rounds {
			for _, t := range r.Themes {
				for _, q := range t.Questions {
					if q.Scenario != nil || q.TypeElem != nil {
						ver = 4
					}
				}
			}
		}
	}
	if ver > 5 {
		im.rep.warnf(CodeUnsupportedVersion, "", fmt.Sprintf("package version %s is newer than 5; parsed with v5 rules", xp.Version))
		ver = 5
	}
	im.legacy = ver < 5
	im.rep.Version = ver
}

func newID() string { return uuid.Must(uuid.NewV7()).String() }

func (im *importer) mapPackage(xp *xmlPackage) (*packs.Pack, error) {
	p := &packs.Pack{
		ID:          newID(),
		Name:        strings.TrimSpace(xp.Name),
		Language:    strings.TrimSpace(xp.Language),
		Restriction: strings.TrimSpace(xp.Restriction),
		Date:        strings.TrimSpace(xp.Date),
		Publisher:   strings.TrimSpace(xp.Publisher),
		ContactURI:  strings.TrimSpace(xp.ContactURI),
		SIQID:       strings.TrimSpace(xp.ID),
		Tags:        []string{},
		Info:        mapInfo(xp.Info),
		Rounds:      []packs.Round{},
	}
	if p.Name == "" {
		p.Name = "Untitled package"
	}
	if d, err := strconv.Atoi(strings.TrimSpace(xp.Difficulty)); err == nil && d >= 0 {
		p.Difficulty = d
	}
	for _, t := range xp.Tags {
		if t = strings.TrimSpace(t); t != "" {
			p.Tags = append(p.Tags, t)
		}
	}
	if logo := strings.TrimSpace(xp.Logo); logo != "" {
		if strings.HasPrefix(logo, "@") {
			if id, ok := im.importMedia(packs.ContentImage, logo[1:], "/logo"); ok {
				p.LogoMediaID = id
			}
		} else {
			p.LogoURL = logo
			im.rep.infof(CodeExternalURL, "/logo", "logo is an external URL")
		}
	}
	if err := im.mapGlobal(xp, p); err != nil {
		return nil, err
	}
	for ri, xr := range xp.Rounds {
		p.Rounds = append(p.Rounds, im.mapRound(&xr, fmt.Sprintf("/rounds/%d", ri)))
	}
	if im.upgraded > 0 {
		im.rep.infof(CodeV4Upgraded, "", fmt.Sprintf("package version %d: %d question(s) converted to the v5 model", im.rep.Version, im.upgraded))
	}
	if err := im.ctx.Err(); err != nil {
		return nil, err
	}
	return p, nil
}

// mapGlobal fills Extra from v5 <global> or the v4 Texts/*.xml files.
func (im *importer) mapGlobal(xp *xmlPackage, p *packs.Pack) error {
	var authors []xmlAuthor
	var sources []xmlSource
	if xp.Global != nil {
		authors, sources = xp.Global.Authors, xp.Global.Sources
	}
	if len(authors) == 0 && im.authorsFile != nil {
		data, err := readEntry(im.authorsFile, maxContentXMLSize)
		if err != nil {
			return err
		}
		if authors, err = decodeAuthorsFile(data); err != nil {
			return err
		}
	}
	if len(sources) == 0 && im.sourcesFile != nil {
		data, err := readEntry(im.sourcesFile, maxContentXMLSize)
		if err != nil {
			return err
		}
		if sources, err = decodeSourcesFile(data); err != nil {
			return err
		}
	}
	if len(authors) == 0 && len(sources) == 0 {
		return nil
	}
	ex := &packs.Extra{}
	for _, a := range authors {
		ex.GlobalAuthors = append(ex.GlobalAuthors, packs.AuthorRecord{
			ID: a.ID, Name: a.Name, SecondName: a.SecondName, Surname: a.Surname, Country: a.Country, City: a.City,
		})
	}
	for _, s := range sources {
		ex.GlobalSources = append(ex.GlobalSources, packs.SourceRecord{
			ID: s.ID, Author: s.Author, Title: s.Title, Year: s.Year, Publish: s.Publish, City: s.City,
		})
	}
	p.Extra = ex
	return nil
}

func mapInfo(x *xmlInfo) packs.Info {
	if x == nil {
		return packs.Info{}
	}
	info := packs.Info{Comments: x.Comments, ShowmanComments: x.ShowmanComments}
	if len(x.Authors) > 0 {
		info.Authors = append([]string(nil), x.Authors...)
	}
	if len(x.Sources) > 0 {
		info.Sources = append([]string(nil), x.Sources...)
	}
	return info
}

func (im *importer) mapRound(xr *xmlRound, path string) packs.Round {
	r := packs.Round{ID: newID(), Name: strings.TrimSpace(xr.Name), Info: mapInfo(xr.Info), Themes: []packs.Theme{}}
	switch strings.ToLower(strings.TrimSpace(xr.Type)) {
	case "", "standart", "standard", "table":
		r.Type = packs.RoundStandard
	case "final", "themelist":
		r.Type = packs.RoundFinal
	default:
		r.Type = packs.RoundStandard
		im.rep.warnf(CodeUnknownRoundType, path, fmt.Sprintf("unknown round type %q; standard assumed", xr.Type))
	}
	im.rep.Stats.Rounds++
	for ti, xt := range xr.Themes {
		r.Themes = append(r.Themes, im.mapTheme(&xt, fmt.Sprintf("%s/themes/%d", path, ti)))
	}
	return r
}

func (im *importer) mapTheme(xt *xmlTheme, path string) packs.Theme {
	t := packs.Theme{ID: newID(), Name: strings.TrimSpace(xt.Name), Info: mapInfo(xt.Info), Questions: []packs.Question{}}
	im.rep.Stats.Themes++
	for qi, xq := range xt.Questions {
		t.Questions = append(t.Questions, im.mapQuestion(&xq, fmt.Sprintf("%s/questions/%d", path, qi)))
	}
	return t
}

func (im *importer) mapQuestion(xq *xmlQuestion, path string) packs.Question {
	price := 0
	if v, err := strconv.Atoi(strings.TrimSpace(xq.Price)); err == nil {
		price = v
	} else if f, err := strconv.ParseFloat(strings.TrimSpace(xq.Price), 64); err == nil {
		price = int(f)
	} else {
		im.rep.warnf(CodeInvalidPrice, path, fmt.Sprintf("invalid price %q; 0 used", xq.Price))
	}
	if price < 0 {
		price = -1
	}
	q := packs.Question{
		ID:     newID(),
		Price:  price,
		Info:   mapInfo(xq.Info),
		Params: packs.QuestionParams{Question: []packs.ContentItem{}},
		Right:  []string{},
	}

	// Legacy model: no <params> in a v4 package, or <scenario>/<type> present
	// in any package. Mixed documents (a v5 <params> plus a v4 <type>) are
	// handled by merging the legacy type below.
	legacyRes := legacyTypeResult{}
	if xq.Params == nil && (im.legacy || xq.Scenario != nil || xq.TypeElem != nil) {
		xq, legacyRes = upgradeQuestion(xq, price)
		im.upgraded++
	} else if xq.TypeElem != nil {
		legacyRes = upgradeLegacyType(xq.TypeElem.Name, xq.TypeElem.Params, price)
		cp := *xq
		cp.Type = legacyRes.typeName
		cp.Params = &xmlParams{Params: append(append([]xmlParam{}, legacyRes.params...), xq.Params.Params...)}
		xq = &cp
		im.upgraded++
	}
	if legacyRes.costInvalid != "" {
		im.rep.warnf(CodeInvalidNumberSet, path, fmt.Sprintf("cannot parse cost %q; nominal price used", legacyRes.costInvalid))
	}

	if price == -1 {
		q.Type = packs.QSimple
	} else {
		q.Type = im.normalizeType(xq.Type, path)
	}

	if xq.Params != nil {
		seen := map[string]bool{}
		for _, prm := range xq.Params.Params {
			name := strings.TrimSpace(prm.Name)
			if seen[name] {
				continue // the reference reader keeps the first occurrence
			}
			seen[name] = true
			im.mapKnownParam(&q, prm, name, path)
		}
	}
	if xq.Script != nil && len(xq.Script.Steps) > 0 {
		for _, st := range xq.Script.Steps {
			step := packs.ScriptStep{Type: strings.TrimSpace(st.Type), Params: []packs.Param{}}
			for _, prm := range st.Params {
				step.Params = append(step.Params, im.mapParam(prm, path+"/script/"+strings.TrimSpace(prm.Name)))
			}
			q.Script = append(q.Script, step)
		}
		im.rep.infof(CodeCustomScript, path, fmt.Sprintf("custom script with %d step(s) preserved; not executed by the engine", len(q.Script)))
	}

	if xq.Right != nil {
		q.Right = append(q.Right, xq.Right.Answers...)
	}
	if len(q.Right) == 0 {
		q.Right = []string{""}
	}
	if xq.Wrong != nil && len(xq.Wrong.Answers) > 0 {
		q.Wrong = append([]string(nil), xq.Wrong.Answers...)
	}
	if price >= 0 && q.Type != packs.QSecretNoQuestion && allEmpty(q.Right) {
		im.rep.warnf(CodeEmptyRight, path, "question has no right answer")
	}

	im.rep.Stats.Questions++
	im.rep.Stats.ByType[string(q.EffectiveType())]++
	return q
}

func allEmpty(ss []string) bool {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return false
		}
	}
	return true
}

// normalizeType maps the type attribute (v5 names, reserved aliases and
// legacy v4 names) to a packs.QuestionType; unknown names are kept verbatim.
func (im *importer) normalizeType(name, path string) packs.QuestionType {
	name = strings.TrimSpace(name)
	switch strings.ToLower(name) {
	case "", "simple", "withbutton":
		return packs.QSimple
	case "stake", "auction":
		return packs.QStake
	case "stakeall":
		return packs.QStakeAll
	case "secret", "cat", "bagcat":
		return packs.QSecret
	case "secretpublicprice":
		return packs.QSecretPublicPrice
	case "secretnoquestion":
		return packs.QSecretNoQuestion
	case "norisk", "sponsored", "foryourself":
		return packs.QNoRisk
	case "forall":
		return packs.QForAll
	case "custom":
		return packs.QCustom
	}
	im.rep.warnf(CodeUnknownQuestionType, path, fmt.Sprintf("unknown question type %q; the showman plays it manually", name))
	return packs.QuestionType(name)
}

// mapKnownParam handles one <param> of a question.
func (im *importer) mapKnownParam(q *packs.Question, prm xmlParam, name, path string) {
	ppath := path + "/params/" + name
	switch name {
	case "question":
		q.Params.Question = im.mapContentParam(prm, ppath)
	case "answer":
		if items := im.mapContentParam(prm, ppath); len(items) > 0 {
			q.Params.Answer = items
		}
	case "theme":
		q.Params.Theme = prm.Text
	case "price":
		if ns, ok := parseNumberSetParam(prm); ok {
			q.Params.Price = &ns
		} else {
			im.rep.warnf(CodeInvalidNumberSet, ppath, "cannot parse price number set; nominal price used")
			v := max(q.Price, 0)
			q.Params.Price = &packs.NumberSet{Min: v, Max: v}
		}
	case "selectionMode":
		v := strings.TrimSpace(prm.Text)
		switch strings.ToLower(v) {
		case "any":
			q.Params.SelectionMode = packs.SelectAny
		case "exceptcurrent":
			q.Params.SelectionMode = packs.SelectExceptCurrent
		case "":
		default:
			im.rep.warnf(CodeUnknownParam, ppath, fmt.Sprintf("unknown selectionMode %q kept verbatim", v))
			q.Params.SelectionMode = packs.SelectionMode(v)
		}
	case "answerType":
		v := strings.TrimSpace(prm.Text)
		switch strings.ToLower(v) {
		case "", "text":
			q.Params.AnswerType = ""
		case "select", "number", "point", "client":
			q.Params.AnswerType = packs.AnswerType(strings.ToLower(v))
		default:
			im.rep.warnf(CodeUnknownParam, ppath, fmt.Sprintf("unknown answerType %q kept verbatim", v))
			q.Params.AnswerType = packs.AnswerType(v)
		}
	case "answerOptions":
		for _, sub := range prm.Params {
			label := strings.TrimSpace(sub.Name)
			q.Params.AnswerOptions = append(q.Params.AnswerOptions, packs.AnswerOption{
				Label:   label,
				Content: im.mapContentParam(sub, ppath+"/"+label),
			})
		}
	case "answerDeviation":
		v := strings.TrimSpace(prm.Text)
		if f, err := strconv.ParseFloat(strings.ReplaceAll(v, ",", "."), 64); err == nil {
			q.Params.AnswerDeviation = f
		} else if v != "" {
			im.rep.warnf(CodeUnknownParam, ppath, fmt.Sprintf("invalid answerDeviation %q ignored", v))
		}
	case "answerDuration":
		v := strings.TrimSpace(prm.Text)
		if f, err := strconv.ParseFloat(strings.ReplaceAll(v, ",", "."), 64); err == nil && f > 0 {
			q.Params.AnswerDurationMs = int64(f*1000 + 0.5)
		} else if v != "" {
			im.rep.warnf(CodeUnknownParam, ppath, fmt.Sprintf("invalid answerDuration %q ignored", v))
		}
	default:
		q.Extra = append(q.Extra, im.mapParam(prm, ppath))
		im.rep.warnf(CodeUnknownParam, ppath, fmt.Sprintf("unknown parameter %q preserved verbatim", name))
	}
}

// mapContentParam converts a content parameter; a simple parameter used where
// content is expected becomes a single text item.
func (im *importer) mapContentParam(prm xmlParam, path string) []packs.ContentItem {
	if len(prm.Items) == 0 && strings.TrimSpace(prm.Text) != "" {
		return []packs.ContentItem{{Type: packs.ContentText, Text: prm.Text}}
	}
	return im.mapItems(prm.Items, path)
}

func (im *importer) mapItems(items []xmlItem, path string) []packs.ContentItem {
	out := []packs.ContentItem{}
	for i, x := range items {
		if it, ok := im.mapItem(x, fmt.Sprintf("%s/%d", path, i)); ok {
			out = append(out, it)
		}
	}
	return out
}

// mapItem converts one <item>. Media references are stored; a reference that
// cannot be resolved or stored drops the item (reported as an error).
func (im *importer) mapItem(x xmlItem, path string) (packs.ContentItem, bool) {
	t := strings.ToLower(strings.TrimSpace(x.Type))
	var ct packs.ContentType
	switch t {
	case "", "text":
		ct = packs.ContentText
	case "image", "audio", "video", "html":
		ct = packs.ContentType(t)
	default:
		if strings.TrimSpace(x.Text) == "" {
			im.rep.warnf(CodeMediaUnsupported, path, fmt.Sprintf("unknown content type %q without a value; item dropped", t))
			return packs.ContentItem{}, false
		}
		im.rep.warnf(CodeMediaUnsupported, path, fmt.Sprintf("unknown content type %q; treated as text", t))
		ct = packs.ContentText
	}
	item := packs.ContentItem{Type: ct}

	pl := strings.ToLower(strings.TrimSpace(x.Placement))
	switch pl {
	case "", "screen", "replic", "background":
	default:
		im.rep.warnf(CodeUnknownParam, path, fmt.Sprintf("unknown placement %q ignored", x.Placement))
		pl = ""
	}
	if (ct == packs.ContentAudio && pl == "background") || (ct != packs.ContentAudio && pl == "screen") {
		pl = "" // default placement is stored as empty
	}
	item.Placement = packs.Placement(pl)

	if x.durationMs > 0 {
		item.DurationMs = x.durationMs
	} else if d := strings.TrimSpace(x.Duration); d != "" {
		if ms, ok := parseDuration(d); ok {
			item.DurationMs = ms
		} else {
			im.rep.warnf(CodeUnknownParam, path, fmt.Sprintf("invalid duration %q ignored", d))
		}
	}
	if strings.EqualFold(strings.TrimSpace(x.WaitForFinish), "false") {
		item.NoWait = true
	}

	if ct == packs.ContentText {
		item.Text = x.Text
		return item, true
	}
	val := strings.TrimSpace(x.Text)
	isRef := parseBool(x.IsRef)
	if !isRef && strings.HasPrefix(val, "@") { // v4 habit inside a v5 file
		isRef = true
		val = val[1:]
	}
	if ct == packs.ContentHTML {
		im.rep.infof(CodeHTMLContent, path, "html content; players must allow html")
	}
	if isRef {
		id, ok := im.importMedia(ct, val, path)
		if !ok {
			return item, false
		}
		item.MediaID = id
		return item, true
	}
	if val == "" {
		im.rep.errorf(CodeMediaMissing, path, "empty media reference; item dropped")
		im.rep.Stats.MediaMissing++
		return item, false
	}
	item.URL = val
	im.rep.infof(CodeExternalURL, path, fmt.Sprintf("external %s URL %s", ct, val))
	return item, true
}

// mapParam preserves an arbitrary parameter (unknown question params and
// script params) verbatim, storing referenced media on the way.
func (im *importer) mapParam(x xmlParam, path string) packs.Param {
	p := packs.Param{Name: strings.TrimSpace(x.Name), Type: strings.TrimSpace(x.Type), IsRef: parseBool(x.IsRef)}
	if strings.EqualFold(p.Type, "simple") {
		p.Type = ""
	}
	switch strings.ToLower(p.Type) {
	case "content":
		p.Items = im.mapItems(x.Items, path)
	case "group":
		for _, sub := range x.Params {
			p.Params = append(p.Params, im.mapParam(sub, path+"/"+strings.TrimSpace(sub.Name)))
		}
	case "numberset":
		if ns, ok := parseNumberSetParam(x); ok {
			p.NumberSet = &ns
		} else {
			im.rep.warnf(CodeInvalidNumberSet, path, "cannot parse number set")
		}
	default:
		p.Value = x.Text
		if len(x.Items) > 0 {
			p.Items = im.mapItems(x.Items, path)
		}
	}
	return p
}

// importMedia resolves ref inside the folder of ct and stores it. It returns
// the media ID (empty with SkipMedia) and false when the item must be dropped.
func (im *importer) importMedia(ct packs.ContentType, ref, path string) (string, bool) {
	folder := folderForKind(string(ct))
	f, where := im.index.resolve(folder, ref)
	if f == nil {
		im.rep.errorf(CodeMediaMissing, path, fmt.Sprintf("media file %q not found in %s/; item dropped", ref, folder))
		im.rep.Stats.MediaMissing++
		return "", false
	}
	if where != folder {
		loc := where
		switch where {
		case "":
			loc = "the archive root"
		case "*":
			loc = "an unknown folder"
		}
		im.rep.warnf(CodeExtraFolderEntry, path, fmt.Sprintf("media %q found in %s instead of %s/", ref, loc, folder))
	}
	if im.opts.SkipMedia {
		return "", true
	}
	c, ok := im.cache[f]
	if !ok {
		c = im.storeEntry(f, ct, ref)
		im.cache[f] = c
	}
	if c.code != "" {
		im.rep.errorf(c.code, path, c.msg)
		im.rep.Stats.MediaMissing++
		return "", false
	}
	return c.id, true
}

// errReader records the first read error so that zip-side failures can be
// told apart from store-side failures.
type errReader struct {
	r   io.Reader
	err error
}

func (e *errReader) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err != nil && err != io.EOF && e.err == nil {
		e.err = err
	}
	return n, err
}

// storeEntry streams one zip entry into the media store. Infrastructure
// errors are recorded as fatal in the importer (see mapPackage) by returning a
// cachedMedia with the fatal flag; sentinel store errors become report codes.
func (im *importer) storeEntry(f *zip.File, ct packs.ContentType, ref string) *cachedMedia {
	rc, err := f.Open()
	if err != nil {
		return &cachedMedia{code: CodeMediaMissing, msg: fmt.Sprintf("cannot open %q: %v; item dropped", ref, err)}
	}
	defer rc.Close()
	er := &errReader{r: rc}
	meta, err := im.store.Put(im.ctx, er, media.PutOptions{OriginalName: ref, ExpectedKind: media.Kind(ct)})
	switch {
	case err == nil:
		im.rep.Stats.MediaImported++
		im.rep.Stats.Bytes += meta.Size
		return &cachedMedia{id: meta.ID}
	case er.err != nil:
		return &cachedMedia{code: CodeMediaMissing, msg: fmt.Sprintf("cannot read %q: %v; item dropped", ref, er.err)}
	case errors.Is(err, media.ErrTooLarge):
		return &cachedMedia{code: CodeMediaTooLarge, msg: fmt.Sprintf("media %q rejected: %v; item dropped", ref, err)}
	case errors.Is(err, media.ErrUnsupported), errors.Is(err, media.ErrKindMismatch):
		return &cachedMedia{code: CodeMediaUnsupported, msg: fmt.Sprintf("media %q rejected: %v; item dropped", ref, err)}
	case im.ctx.Err() != nil:
		return &cachedMedia{code: CodeMediaMissing, msg: fmt.Sprintf("media %q: %v", ref, err)}
	default:
		// Unknown store failure: report it and keep going; the pack is still usable.
		return &cachedMedia{code: CodeMediaMissing, msg: fmt.Sprintf("media %q could not be stored: %v; item dropped", ref, err)}
	}
}

// parseBool accepts true/True/TRUE and everything else as false.
func parseBool(s string) bool {
	return strings.EqualFold(strings.TrimSpace(s), "true")
}

// parseNumberSetParam reads <numberSet minimum maximum step/>; as a fallback
// a simple text value in v4 cost syntax is accepted.
func parseNumberSetParam(prm xmlParam) (packs.NumberSet, bool) {
	if prm.NumberSet != nil {
		ns := packs.NumberSet{}
		var ok1, ok2, ok3 bool
		ns.Min, ok1 = parseIntAttr(prm.NumberSet.Minimum)
		ns.Max, ok2 = parseIntAttr(prm.NumberSet.Maximum)
		ns.Step, ok3 = parseIntAttr(prm.NumberSet.Step)
		if ok1 && ok2 && ok3 {
			return ns, true
		}
		return packs.NumberSet{}, false
	}
	if x, ok := parseV4Cost(prm.Text); ok {
		mn, _ := strconv.Atoi(x.Minimum)
		mx, _ := strconv.Atoi(x.Maximum)
		st, _ := strconv.Atoi(x.Step)
		return packs.NumberSet{Min: mn, Max: mx, Step: st}, true
	}
	return packs.NumberSet{}, false
}

// parseIntAttr parses an optional integer attribute (missing = 0).
func parseIntAttr(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, true
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// parseDuration accepts hh:mm:ss (and the other TimeSpan spellings
// [d.]h:m[:s[.fff]]) plus a bare number of seconds. It returns milliseconds.
func parseDuration(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "-") {
		return 0, false
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		if f < 0 {
			return 0, false
		}
		return int64(f*1000 + 0.5), true
	}
	days := int64(0)
	if i := strings.IndexByte(s, '.'); i > 0 && !strings.Contains(s[:i], ":") {
		d, err := strconv.ParseInt(s[:i], 10, 64)
		if err != nil {
			return 0, false
		}
		days = d
		s = s[i+1:]
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	var h, m int64
	var sec float64
	var err error
	if h, err = strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64); err != nil {
		return 0, false
	}
	if m, err = strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64); err != nil {
		return 0, false
	}
	if len(parts) == 3 {
		if sec, err = strconv.ParseFloat(strings.TrimSpace(parts[2]), 64); err != nil {
			return 0, false
		}
	}
	if h < 0 || m < 0 || sec < 0 {
		return 0, false
	}
	total := float64(days*86400+h*3600+m*60)*1000 + sec*1000
	return int64(total + 0.5), true
}
