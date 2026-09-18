package media

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	probeTimeout    = 10 * time.Second
	maxOriginalName = 255
)

// DiskStore keeps bytes under dir/<id[:2]>/<id>.<ext> and metadata in the
// SQLite table "media" (created by internal/db migrations).
type DiskStore struct {
	db     *sql.DB
	dir    string
	tmpDir string
	limits Limits
	prober Prober
	log    *slog.Logger
	now    func() time.Time
}

var _ Store = (*DiskStore)(nil)

// NewDiskStore prepares dir (and dir/tmp) and removes stale temp files left by
// a previous crash. A nil prober means NopProber; a nil logger means
// slog.Default().
func NewDiskStore(db *sql.DB, dir string, limits Limits, prober Prober, logger *slog.Logger) (*DiskStore, error) {
	if db == nil {
		return nil, errors.New("media: nil db")
	}
	if dir == "" {
		return nil, errors.New("media: empty dir")
	}
	if prober == nil {
		prober = NopProber{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	tmpDir := filepath.Join(dir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return nil, fmt.Errorf("media: mkdir %s: %w", tmpDir, err)
	}
	if entries, err := os.ReadDir(tmpDir); err == nil {
		for _, e := range entries {
			if err := os.RemoveAll(filepath.Join(tmpDir, e.Name())); err != nil {
				logger.Warn("media: remove stale temp file", "name", e.Name(), "err", err)
			}
		}
	}
	return &DiskStore{
		db:     db,
		dir:    dir,
		tmpDir: tmpDir,
		limits: limits,
		prober: prober,
		log:    logger,
		now:    time.Now,
	}, nil
}

// Dir returns the root directory of the store.
func (s *DiskStore) Dir() string { return s.dir }

// Path returns the on-disk location of a stored object, or "" if the id is
// unknown. It performs a metadata lookup (the extension is part of the name).
func (s *DiskStore) Path(id string) string {
	m, err := s.Stat(context.Background(), id)
	if err != nil {
		return ""
	}
	return s.filePath(id, m.Ext)
}

func (s *DiskStore) filePath(id, ext string) string {
	return filepath.Join(s.dir, id[:2], id+"."+ext)
}

// IsValidID reports whether id is a 64-character lowercase hex sha256.
func IsValidID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Put implements Store. The body is streamed once into a temp file while
// being hashed; the first SniffLen bytes decide the kind (and therefore the
// size limit) before anything is written, so mismatches and oversize uploads
// abort early.
func (s *DiskStore) Put(ctx context.Context, r io.Reader, opts PutOptions) (Meta, error) {
	name := cleanOriginalName(opts.OriginalName)

	head := make([]byte, SniffLen)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return Meta{}, fmt.Errorf("media: read body: %w", err)
	}
	head = head[:n]

	kind, mime, ext, err := Sniff(head, name)
	if err != nil {
		return Meta{}, err
	}
	if ext == "svg" && !s.limits.AllowSVG {
		return Meta{}, fmt.Errorf("%w: image/svg+xml is disabled", ErrUnsupported)
	}
	if opts.ExpectedKind != "" && opts.ExpectedKind != kind {
		return Meta{}, fmt.Errorf("%w: content is %s, expected %s", ErrKindMismatch, kind, opts.ExpectedKind)
	}
	limit := s.limits.Max(kind)
	if limit > 0 && int64(len(head)) > limit {
		return Meta{}, fmt.Errorf("%w: %s limit is %d bytes", ErrTooLarge, kind, limit)
	}

	tmp, err := os.CreateTemp(s.tmpDir, "put-*")
	if err != nil {
		return Meta{}, fmt.Errorf("media: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	hash := sha256.New()
	w := io.MultiWriter(tmp, hash)
	if _, err := w.Write(head); err != nil {
		return Meta{}, fmt.Errorf("media: write temp file: %w", err)
	}
	size := int64(len(head))
	var body io.Reader = r
	if limit > 0 {
		body = io.LimitReader(r, limit-size+1) // one extra byte detects overflow
	}
	copied, err := io.Copy(w, body)
	if err != nil {
		return Meta{}, fmt.Errorf("media: write temp file: %w", err)
	}
	size += copied
	if limit > 0 && size > limit {
		return Meta{}, fmt.Errorf("%w: %s limit is %d bytes", ErrTooLarge, kind, limit)
	}
	if err := ctx.Err(); err != nil {
		return Meta{}, err
	}
	if err := tmp.Sync(); err != nil {
		return Meta{}, fmt.Errorf("media: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return Meta{}, fmt.Errorf("media: close temp file: %w", err)
	}
	id := hex.EncodeToString(hash.Sum(nil))

	// Dedup: identical content already stored.
	existing, err := s.Stat(ctx, id)
	switch {
	case err == nil:
		return existing, nil
	case !errors.Is(err, ErrNotFound):
		return Meta{}, err
	}

	info := s.probe(ctx, tmpPath, kind, mime)

	final := s.filePath(id, ext)
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return Meta{}, fmt.Errorf("media: mkdir: %w", err)
	}
	if err := os.Rename(tmpPath, final); err != nil {
		return Meta{}, fmt.Errorf("media: move into store: %w", err)
	}
	keep = true // temp file no longer exists

	m := Meta{
		ID:           id,
		Kind:         kind,
		MIME:         mime,
		Ext:          ext,
		Size:         size,
		DurationMs:   info.DurationMs,
		Width:        info.Width,
		Height:       info.Height,
		OriginalName: name,
		RefCount:     0,
		CreatedAt:    s.now().UnixMilli(),
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO media
		(id, kind, mime, ext, size, duration_ms, width, height, original_name, ref_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)
		ON CONFLICT(id) DO NOTHING`,
		m.ID, string(m.Kind), m.MIME, m.Ext, m.Size, m.DurationMs, m.Width, m.Height, m.OriginalName, m.CreatedAt)
	if err != nil {
		return Meta{}, fmt.Errorf("media: insert %s: %w", id, err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		// Lost a race with a concurrent Put of the same content.
		return s.Stat(ctx, id)
	}
	return m, nil
}

// probe fills duration/dimensions best-effort. Images are decoded with the
// stdlib first (cheap, no subprocess); the Prober is the fallback for
// everything else. Probe failures are logged and ignored.
func (s *DiskStore) probe(ctx context.Context, path string, kind Kind, mime string) Info {
	var info Info
	switch kind {
	case KindHTML:
		return info
	case KindImage:
		if w, h, err := imageSizeFile(path, mime); err == nil {
			info.Width, info.Height = w, h
			return info
		}
	}
	if _, nop := s.prober.(NopProber); nop {
		return info
	}
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	probed, err := s.prober.Probe(pctx, path)
	if err != nil {
		s.log.Warn("media: probe failed", "mime", mime, "err", err)
		return info
	}
	if kind == KindImage {
		probed.DurationMs = 0 // ffprobe reports one frame worth of "duration" for stills
	}
	return probed
}

const selectMeta = `SELECT id, kind, mime, ext, size, duration_ms, width, height, original_name, ref_count, created_at
	FROM media WHERE id = ?`

// Stat implements Store.
func (s *DiskStore) Stat(ctx context.Context, id string) (Meta, error) {
	if !IsValidID(id) {
		return Meta{}, ErrNotFound
	}
	var m Meta
	var kind string
	err := s.db.QueryRowContext(ctx, selectMeta, id).Scan(
		&m.ID, &kind, &m.MIME, &m.Ext, &m.Size, &m.DurationMs, &m.Width, &m.Height, &m.OriginalName, &m.RefCount, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Meta{}, ErrNotFound
	}
	if err != nil {
		return Meta{}, fmt.Errorf("media: stat %s: %w", id, err)
	}
	m.Kind = Kind(kind)
	return m, nil
}

// Open implements Store. A row whose file vanished yields ErrNotFound.
func (s *DiskStore) Open(ctx context.Context, id string) (io.ReadSeekCloser, Meta, error) {
	m, err := s.Stat(ctx, id)
	if err != nil {
		return nil, Meta{}, err
	}
	f, err := os.Open(s.filePath(id, m.Ext))
	if errors.Is(err, fs.ErrNotExist) {
		s.log.Warn("media: file missing for stored row", "id", id)
		return nil, Meta{}, fmt.Errorf("media: open %s: file missing: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, Meta{}, fmt.Errorf("media: open %s: %w", id, err)
	}
	return f, m, nil
}

// Delete implements Store. The row is removed with an atomic ref_count guard
// first, then the file (a missing file is tolerated); a crash in between
// leaves at most an orphan file, never a row without bytes.
func (s *DiskStore) Delete(ctx context.Context, id string) error {
	if !IsValidID(id) {
		return ErrNotFound
	}
	var ext string
	var refs int
	err := s.db.QueryRowContext(ctx, `SELECT ext, ref_count FROM media WHERE id = ?`, id).Scan(&ext, &refs)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("media: delete %s: %w", id, err)
	}
	if refs > 0 {
		return fmt.Errorf("%w: %d reference(s)", ErrInUse, refs)
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM media WHERE id = ? AND ref_count = 0`, id)
	if err != nil {
		return fmt.Errorf("media: delete %s: %w", id, err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		// Referenced (or removed) concurrently between the two statements.
		if _, err := s.Stat(ctx, id); errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return ErrInUse
	}
	path := s.filePath(id, ext)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("media: remove %s: %w", path, err)
	}
	return nil
}

// Sweep deletes unreferenced objects created more than olderThan ago (the
// grace period lets an upload be attached to a pack before GC). It returns
// the number of deleted objects; per-object failures are joined into err.
func (s *DiskStore) Sweep(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := s.now().Add(-olderThan).UnixMilli()
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM media WHERE ref_count = 0 AND created_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("media: sweep: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("media: sweep: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close() // release the connection before deleting
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("media: sweep: %w", err)
	}

	deleted := 0
	var errs []error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return deleted, err
		}
		err := s.Delete(ctx, id)
		switch {
		case err == nil:
			deleted++
		case errors.Is(err, ErrInUse), errors.Is(err, ErrNotFound):
			// referenced or removed since the scan; skip
		default:
			errs = append(errs, err)
		}
	}
	return deleted, errors.Join(errs...)
}

// cleanOriginalName strips directories and control characters and caps the
// length; the store keeps it for reporting only.
func cleanOriginalName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if runes := []rune(name); len(runes) > maxOriginalName {
		name = string(runes[:maxOriginalName])
	}
	return name
}
