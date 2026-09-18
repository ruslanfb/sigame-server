package packs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"sigame/internal/db"
)

// Repo stores packs in SQLite. The full pack is kept as JSON in packs.doc;
// the remaining columns are derived from Summarize and exist only for
// listing, filtering and sorting. Media reference counts (media.ref_count)
// and the pack_media join table are maintained here, inside the same
// transaction as the pack row, so that media.Store can refuse to delete
// objects that are still in use.
//
// The repository relies on the ulower() SQL function that internal/db
// registers on every connection (Unicode-aware case folding for search and
// name sorting).
type Repo struct {
	db  *sql.DB
	now func() int64 // Unix ms UTC; replaceable in tests
}

// NewRepo wraps an opened and migrated database.
func NewRepo(sqlDB *sql.DB) *Repo {
	return &Repo{db: sqlDB, now: func() int64 { return time.Now().UnixMilli() }}
}

// ListFilter selects and orders packs for Repo.List. Zero values mean
// "no constraint".
type ListFilter struct {
	Query         string // case-insensitive substring of name, authors or tags
	Language      string // exact match on the language tag
	Tag           string // case-insensitive exact match of one tag
	MinDifficulty int    // inclusive lower bound
	MaxDifficulty int    // inclusive upper bound; 0 = unbounded
	HasMedia      *bool  // nil = both
	Sort          string // "updatedAt" (default) | "name" | "createdAt"
	Order         string // "asc" | "desc"; default: desc for timestamps, asc for name
	Limit         int    // default DefaultListLimit, capped at MaxListLimit
	Offset        int
}

// ListResult is a page of summaries plus the total number of matches.
type ListResult struct {
	Items []Summary `json:"items"`
	Total int       `json:"total" doc:"Total number of packs matching the filter, ignoring limit/offset"`
}

// Paging limits for List.
const (
	DefaultListLimit = 50
	MaxListLimit     = 200
)

// Sort keys accepted by ListFilter.Sort.
const (
	SortUpdatedAt = "updatedAt"
	SortName      = "name"
	SortCreatedAt = "createdAt"
)

// listSeparator joins tags and authors inside their search columns.
const listSeparator = "\n"

// summaryColumns are the columns scanned by scanSummary, in order.
// restriction has no column of its own and is read from the JSON document.
const summaryColumns = `id, version, name, language, difficulty,
	COALESCE(json_extract(doc, '$.restriction'), ''), tags, authors,
	round_count, theme_count, question_count, has_media, logo_media_id, created_at, updated_at`

// Create validates and inserts a new pack. Empty IDs are generated, the pack
// is normalised, Version is set to 1 and both timestamps to now. Every media
// id referenced by the pack must already exist in the media table; otherwise
// a *ValidationError with code mediaNotFound is returned and nothing is
// written. On success p carries the final IDs, version and timestamps.
func (r *Repo) Create(ctx context.Context, p *Pack) error {
	if p == nil {
		return errors.New("packs: create: nil pack")
	}
	EnsureIDs(p)
	Normalize(p)
	if err := Validate(p); err != nil {
		return err
	}
	now := r.now()
	p.Version = 1
	p.CreatedAt = now
	p.UpdatedAt = now
	doc, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("packs: create: encode: %w", err)
	}
	s := Summarize(p)
	refs := p.MediaIDs()

	return db.Tx(ctx, r.db, func(tx *sql.Tx) error {
		var one int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM packs WHERE id = ?`, p.ID).Scan(&one)
		switch {
		case err == nil:
			return ErrAlreadyExists
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("packs: create: lookup: %w", err)
		}
		if err := checkMediaExist(ctx, tx, p, refs); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO packs (id, version, name, language, difficulty, authors, tags,
				round_count, theme_count, question_count, has_media, logo_media_id, doc, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.ID, s.Version, s.Name, s.Language, s.Difficulty, joinList(s.Authors), joinList(s.Tags),
			s.RoundCount, s.ThemeCount, s.QuestionCount, boolInt(s.HasMedia), s.LogoMediaID, string(doc),
			s.CreatedAt, s.UpdatedAt); err != nil {
			return fmt.Errorf("packs: create: insert: %w", err)
		}
		if err := addMediaRefs(ctx, tx, p.ID, refs); err != nil {
			return fmt.Errorf("packs: create: %w", err)
		}
		return nil
	})
}

// Get loads a full pack by id.
func (r *Repo) Get(ctx context.Context, id string) (*Pack, error) {
	var doc []byte
	err := r.db.QueryRowContext(ctx, `SELECT doc FROM packs WHERE id = ?`, id).Scan(&doc)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("packs: get %s: %w", id, err)
	}
	var p Pack
	if err := json.Unmarshal(doc, &p); err != nil {
		return nil, fmt.Errorf("packs: get %s: decode: %w", id, err)
	}
	return &p, nil
}

// GetSummary loads the metadata view of a pack by id.
func (r *Repo) GetSummary(ctx context.Context, id string) (Summary, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+summaryColumns+` FROM packs WHERE id = ?`, id)
	s, err := scanSummary(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Summary{}, ErrNotFound
	}
	if err != nil {
		return Summary{}, fmt.Errorf("packs: get summary %s: %w", id, err)
	}
	return s, nil
}

// Update replaces a stored pack. expectedVersion must equal the stored
// version (ErrVersionConflict otherwise); 0 skips the check. The stored
// CreatedAt is preserved, Version is incremented, UpdatedAt set to now, and
// media reference counts are adjusted for the ids that were added or removed.
// Newly referenced media must exist (mediaNotFound otherwise). On success p
// carries the new version and timestamp.
func (r *Repo) Update(ctx context.Context, p *Pack, expectedVersion int) error {
	if p == nil {
		return errors.New("packs: update: nil pack")
	}
	if p.ID == "" {
		return ErrNotFound
	}
	return db.Tx(ctx, r.db, func(tx *sql.Tx) error {
		var storedVersion int
		var createdAt int64
		err := tx.QueryRowContext(ctx, `SELECT version, created_at FROM packs WHERE id = ?`, p.ID).
			Scan(&storedVersion, &createdAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("packs: update %s: lookup: %w", p.ID, err)
		}
		if expectedVersion != 0 && storedVersion != expectedVersion {
			return ErrVersionConflict
		}

		// Persisted identity wins over whatever the client sent.
		p.Version = storedVersion
		p.CreatedAt = createdAt
		EnsureIDs(p)
		Normalize(p)
		if err := Validate(p); err != nil {
			return err
		}

		oldRefs, err := mediaRefsTx(ctx, tx, p.ID)
		if err != nil {
			return fmt.Errorf("packs: update %s: %w", p.ID, err)
		}
		newRefs := p.MediaIDs()
		added, removed := diffRefs(oldRefs, newRefs)
		if err := checkMediaExist(ctx, tx, p, added); err != nil {
			return err
		}

		p.Version = storedVersion + 1
		p.UpdatedAt = r.now()
		doc, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("packs: update %s: encode: %w", p.ID, err)
		}
		s := Summarize(p)
		res, err := tx.ExecContext(ctx, `
			UPDATE packs SET version = ?, name = ?, language = ?, difficulty = ?, authors = ?, tags = ?,
				round_count = ?, theme_count = ?, question_count = ?, has_media = ?, logo_media_id = ?,
				doc = ?, updated_at = ?
			WHERE id = ? AND version = ?`,
			s.Version, s.Name, s.Language, s.Difficulty, joinList(s.Authors), joinList(s.Tags),
			s.RoundCount, s.ThemeCount, s.QuestionCount, boolInt(s.HasMedia), s.LogoMediaID,
			string(doc), s.UpdatedAt, p.ID, storedVersion)
		if err != nil {
			return fmt.Errorf("packs: update %s: %w", p.ID, err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return fmt.Errorf("packs: update %s: %w", p.ID, err)
		} else if n != 1 {
			return ErrVersionConflict
		}
		if err := removeMediaRefs(ctx, tx, p.ID, removed); err != nil {
			return fmt.Errorf("packs: update %s: %w", p.ID, err)
		}
		if err := addMediaRefs(ctx, tx, p.ID, added); err != nil {
			return fmt.Errorf("packs: update %s: %w", p.ID, err)
		}
		return nil
	})
}

// Delete removes a pack and releases its media references.
func (r *Repo) Delete(ctx context.Context, id string) error {
	return db.Tx(ctx, r.db, func(tx *sql.Tx) error {
		refs, err := mediaRefsTx(ctx, tx, id)
		if err != nil {
			return fmt.Errorf("packs: delete %s: %w", id, err)
		}
		if err := removeMediaRefs(ctx, tx, id, refs); err != nil {
			return fmt.Errorf("packs: delete %s: %w", id, err)
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM packs WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("packs: delete %s: %w", id, err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return fmt.Errorf("packs: delete %s: %w", id, err)
		} else if n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// Duplicate stores a deep copy of the pack with fresh IDs for the pack, its
// rounds, themes and questions. An empty newName yields "<name> (copy)". The
// SIQ package id is cleared so that an export produces a distinct package.
func (r *Repo) Duplicate(ctx context.Context, id, newName string) (*Pack, error) {
	src, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	cp, err := clonePack(src)
	if err != nil {
		return nil, fmt.Errorf("packs: duplicate %s: %w", id, err)
	}
	ResetIDs(cp)
	cp.SIQID = ""
	if newName = strings.TrimSpace(newName); newName != "" {
		cp.Name = newName
	} else {
		cp.Name = src.Name + " (copy)"
	}
	// CreatedAt/Version stay non-zero until Create resets them, so Normalize
	// treats the copy as an existing pack and keeps an explicit difficulty 0.
	if err := r.Create(ctx, cp); err != nil {
		return nil, err
	}
	return cp, nil
}

// List returns one page of summaries matching f plus the total match count.
func (r *Repo) List(ctx context.Context, f ListFilter) (ListResult, error) {
	var where []string
	var args []any
	if q := strings.TrimSpace(f.Query); q != "" {
		pattern := "%" + escapeLike(strings.ToLower(q)) + "%"
		where = append(where, `(ulower(name) LIKE ? ESCAPE '\' OR ulower(authors) LIKE ? ESCAPE '\' OR ulower(tags) LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern, pattern)
	}
	if f.Language != "" {
		where = append(where, `language = ?`)
		args = append(args, f.Language)
	}
	if tag := strings.TrimSpace(f.Tag); tag != "" {
		where = append(where, `instr(char(10) || ulower(tags) || char(10), ?) > 0`)
		args = append(args, listSeparator+strings.ToLower(tag)+listSeparator)
	}
	if f.MinDifficulty > 0 {
		where = append(where, `difficulty >= ?`)
		args = append(args, f.MinDifficulty)
	}
	if f.MaxDifficulty > 0 {
		where = append(where, `difficulty <= ?`)
		args = append(args, f.MaxDifficulty)
	}
	if f.HasMedia != nil {
		where = append(where, `has_media = ?`)
		args = append(args, boolInt(*f.HasMedia))
	}
	cond := ""
	if len(where) > 0 {
		cond = " WHERE " + strings.Join(where, " AND ")
	}

	var orderBy string
	var dir string
	switch strings.ToLower(f.Order) {
	case "asc":
		dir = "ASC"
	case "desc":
		dir = "DESC"
	case "":
	default:
		return ListResult{}, fmt.Errorf("packs: list: invalid order %q", f.Order)
	}
	switch f.Sort {
	case "", SortUpdatedAt:
		if dir == "" {
			dir = "DESC"
		}
		orderBy = "updated_at " + dir + ", id " + dir
	case SortCreatedAt:
		if dir == "" {
			dir = "DESC"
		}
		orderBy = "created_at " + dir + ", id " + dir
	case SortName:
		if dir == "" {
			dir = "ASC"
		}
		orderBy = "ulower(name) " + dir + ", name " + dir + ", id " + dir
	default:
		return ListResult{}, fmt.Errorf("packs: list: invalid sort %q", f.Sort)
	}

	limit := f.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	if limit > MaxListLimit {
		limit = MaxListLimit
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	out := ListResult{Items: []Summary{}}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM packs`+cond, args...).Scan(&out.Total); err != nil {
		return ListResult{}, fmt.Errorf("packs: list: count: %w", err)
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+summaryColumns+` FROM packs`+cond+` ORDER BY `+orderBy+` LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return ListResult{}, fmt.Errorf("packs: list: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		s, err := scanSummary(rows)
		if err != nil {
			return ListResult{}, fmt.Errorf("packs: list: scan: %w", err)
		}
		out.Items = append(out.Items, s)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, fmt.Errorf("packs: list: %w", err)
	}
	return out, nil
}

// MediaRefs returns the sorted media ids referenced by a pack, as recorded
// in pack_media. ErrNotFound when the pack does not exist.
func (r *Repo) MediaRefs(ctx context.Context, packID string) ([]string, error) {
	var one int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM packs WHERE id = ?`, packID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("packs: media refs %s: %w", packID, err)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT media_id FROM pack_media WHERE pack_id = ? ORDER BY media_id`, packID)
	if err != nil {
		return nil, fmt.Errorf("packs: media refs %s: %w", packID, err)
	}
	defer rows.Close()
	return collectStrings(rows)
}

// --- helpers -------------------------------------------------------------

type scanner interface{ Scan(dest ...any) error }

func scanSummary(row scanner) (Summary, error) {
	var s Summary
	var tags, authors string
	var hasMedia int
	if err := row.Scan(&s.ID, &s.Version, &s.Name, &s.Language, &s.Difficulty, &s.Restriction,
		&tags, &authors, &s.RoundCount, &s.ThemeCount, &s.QuestionCount, &hasMedia, &s.LogoMediaID,
		&s.CreatedAt, &s.UpdatedAt); err != nil {
		return Summary{}, err
	}
	s.Tags = splitList(tags)
	s.Authors = splitList(authors)
	s.HasMedia = hasMedia != 0
	return s, nil
}

func joinList(list []string) string { return strings.Join(list, listSeparator) }

func splitList(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, listSeparator)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// escapeLike escapes the LIKE wildcards and the escape character itself.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func clonePack(src *Pack) (*Pack, error) {
	raw, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}
	var cp Pack
	if err := json.Unmarshal(raw, &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

func collectStrings(rows *sql.Rows) ([]string, error) {
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func mediaRefsTx(ctx context.Context, tx *sql.Tx, packID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT media_id FROM pack_media WHERE pack_id = ?`, packID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectStrings(rows)
}

// diffRefs returns the ids present only in newRefs (added) and only in
// oldRefs (removed), each without duplicates.
func diffRefs(oldRefs, newRefs []string) (added, removed []string) {
	oldSet := make(map[string]struct{}, len(oldRefs))
	for _, id := range oldRefs {
		oldSet[id] = struct{}{}
	}
	newSet := make(map[string]struct{}, len(newRefs))
	for _, id := range newRefs {
		if _, dup := newSet[id]; dup {
			continue
		}
		newSet[id] = struct{}{}
		if _, ok := oldSet[id]; !ok {
			added = append(added, id)
		}
	}
	seen := make(map[string]struct{}, len(oldRefs))
	for _, id := range oldRefs {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		if _, ok := newSet[id]; !ok {
			removed = append(removed, id)
		}
	}
	return added, removed
}

// checkMediaExist verifies that every id in ids has a media row and reports
// each missing one at every place the pack references it.
func checkMediaExist(ctx context.Context, tx *sql.Tx, p *Pack, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `SELECT 1 FROM media WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("packs: media lookup: %w", err)
	}
	defer stmt.Close()
	missing := map[string]bool{}
	for _, id := range ids {
		var one int
		err := stmt.QueryRowContext(ctx, id).Scan(&one)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			missing[id] = true
		case err != nil:
			return fmt.Errorf("packs: media lookup %s: %w", id, err)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	v := &ValidationError{}
	for _, ref := range mediaRefPaths(p) {
		if missing[ref.id] {
			v.add(ref.path, CodeMediaNotFound, "media %s is not stored on this server", ref.id)
		}
	}
	return v
}

func addMediaRefs(ctx context.Context, tx *sql.Tx, packID string, ids []string) error {
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT INTO pack_media (pack_id, media_id) VALUES (?, ?)`, packID, id); err != nil {
			return fmt.Errorf("add media ref %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE media SET ref_count = ref_count + 1 WHERE id = ?`, id); err != nil {
			return fmt.Errorf("increment media ref %s: %w", id, err)
		}
	}
	return nil
}

func removeMediaRefs(ctx context.Context, tx *sql.Tx, packID string, ids []string) error {
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM pack_media WHERE pack_id = ? AND media_id = ?`, packID, id); err != nil {
			return fmt.Errorf("remove media ref %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE media SET ref_count = ref_count - 1 WHERE id = ? AND ref_count > 0`, id); err != nil {
			return fmt.Errorf("decrement media ref %s: %w", id, err)
		}
	}
	return nil
}

// mediaRef is one media reference with the JSON pointer of its location.
type mediaRef struct{ path, id string }

// mediaRefPaths walks the same places as Pack.MediaIDs but keeps duplicates
// and records where each id is used.
func mediaRefPaths(p *Pack) []mediaRef {
	var out []mediaRef
	items := func(path string, list []ContentItem) {
		for i, it := range list {
			if it.MediaID != "" {
				out = append(out, mediaRef{fmt.Sprintf("%s/%d/mediaId", path, i), it.MediaID})
			}
		}
	}
	var params func(path string, list []Param)
	params = func(path string, list []Param) {
		for i, pr := range list {
			pp := fmt.Sprintf("%s/%d", path, i)
			items(pp+"/items", pr.Items)
			params(pp+"/params", pr.Params)
		}
	}
	if p.LogoMediaID != "" {
		out = append(out, mediaRef{"/logoMediaId", p.LogoMediaID})
	}
	for i, r := range p.Rounds {
		for j, t := range r.Themes {
			for k, q := range t.Questions {
				qp := fmt.Sprintf("/rounds/%d/themes/%d/questions/%d", i, j, k)
				items(qp+"/params/question", q.Params.Question)
				items(qp+"/params/answer", q.Params.Answer)
				for o, opt := range q.Params.AnswerOptions {
					items(fmt.Sprintf("%s/params/answerOptions/%d/content", qp, o), opt.Content)
				}
				params(qp+"/extraParams", q.Extra)
				for s, step := range q.Script {
					params(fmt.Sprintf("%s/script/%d/params", qp, s), step.Params)
				}
			}
		}
	}
	return out
}
