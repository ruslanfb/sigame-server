package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func tmpEntries(t *testing.T, st *DiskStore) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(st.Dir(), "tmp"))
	require.NoError(t, err)
	return entries
}

func rowCount(t *testing.T, st *DiskStore) int {
	t.Helper()
	var n int
	require.NoError(t, st.db.QueryRow(`SELECT COUNT(*) FROM media`).Scan(&n))
	return n
}

func TestNewDiskStore(t *testing.T) {
	db := newTestDB(t)
	_, err := NewDiskStore(nil, t.TempDir(), DefaultLimits(), nil, nil)
	require.Error(t, err)
	_, err = NewDiskStore(db, "", DefaultLimits(), nil, nil)
	require.Error(t, err)

	dir := filepath.Join(t.TempDir(), "nested", "media")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tmp"), 0o755))
	stale := filepath.Join(dir, "tmp", "put-stale")
	require.NoError(t, os.WriteFile(stale, []byte("x"), 0o644))

	st, err := NewDiskStore(db, dir, DefaultLimits(), nil, nil)
	require.NoError(t, err)
	require.Equal(t, dir, st.Dir())
	_, err = os.Stat(stale)
	require.ErrorIs(t, err, os.ErrNotExist, "stale temp files are swept on start")
}

func TestPutAndDedup(t *testing.T) {
	st, _ := newTestStore(t, DefaultLimits(), nil)
	ctx := context.Background()
	data := pngBytes(t, 40, 30)
	sum := sha256.Sum256(data)
	wantID := hex.EncodeToString(sum[:])

	before := time.Now().UnixMilli()
	m, err := st.Put(ctx, bytes.NewReader(data), PutOptions{OriginalName: `C:\packs\Images\картинка.png`})
	require.NoError(t, err)
	require.Equal(t, wantID, m.ID)
	require.Equal(t, KindImage, m.Kind)
	require.Equal(t, "image/png", m.MIME)
	require.Equal(t, "png", m.Ext)
	require.Equal(t, int64(len(data)), m.Size)
	require.Equal(t, 40, m.Width)
	require.Equal(t, 30, m.Height)
	require.Equal(t, int64(0), m.DurationMs)
	require.Equal(t, "картинка.png", m.OriginalName, "directories are stripped")
	require.Equal(t, 0, m.RefCount)
	require.GreaterOrEqual(t, m.CreatedAt, before)

	path := st.Path(m.ID)
	require.Equal(t, filepath.Join(st.Dir(), wantID[:2], wantID+".png"), path)
	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, data, onDisk)
	require.Empty(t, tmpEntries(t, st))

	// Same content again: same id, original row untouched, no leftovers.
	again, err := st.Put(ctx, bytes.NewReader(data), PutOptions{OriginalName: "other.png"})
	require.NoError(t, err)
	require.Equal(t, m, again)
	require.Equal(t, 1, rowCount(t, st))
	require.Empty(t, tmpEntries(t, st))

	require.Equal(t, "", st.Path("not-an-id"))
	require.Equal(t, "", st.Path(hex.EncodeToString(bytes.Repeat([]byte{0xab}, 32))), "unknown id")
}

func TestPutKinds(t *testing.T) {
	st, _ := newTestStore(t, DefaultLimits(), nil)
	ctx := context.Background()

	tests := []struct {
		name string
		data []byte
		opts PutOptions
		kind Kind
		ext  string
		w, h int
	}{
		{"jpeg", jpegBytes(t, 9, 8), PutOptions{OriginalName: "a.jpeg"}, KindImage, "jpg", 9, 8},
		{"gif", gifBytes(t, 6, 4), PutOptions{}, KindImage, "gif", 6, 4},
		{"webp", webpVP8X(300, 200), PutOptions{}, KindImage, "webp", 300, 200},
		{"wav", wavBytes(0.05, 8000), PutOptions{ExpectedKind: KindAudio}, KindAudio, "wav", 0, 0},
		{"mp3", mp3Frame(), PutOptions{}, KindAudio, "mp3", 0, 0},
		{"mp4", mp4Header("isom", "isom", "avc1"), PutOptions{ExpectedKind: KindVideo}, KindVideo, "mp4", 0, 0},
		{"webm", ebmlHeader("webm"), PutOptions{}, KindVideo, "webm", 0, 0},
		{"html", []byte("<!DOCTYPE html><p>q</p>"), PutOptions{ExpectedKind: KindHTML}, KindHTML, "html", 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, err := st.Put(ctx, bytes.NewReader(tc.data), tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.kind, m.Kind)
			require.Equal(t, tc.ext, m.Ext)
			require.Equal(t, tc.w, m.Width)
			require.Equal(t, tc.h, m.Height)
			require.FileExists(t, st.Path(m.ID))
		})
	}
	require.Empty(t, tmpEntries(t, st))
}

func TestPutRejections(t *testing.T) {
	st, _ := newTestStore(t, DefaultLimits(), nil)
	ctx := context.Background()

	t.Run("unsupported", func(t *testing.T) {
		_, err := st.Put(ctx, bytes.NewReader([]byte("plain text")), PutOptions{OriginalName: "x.txt"})
		require.ErrorIs(t, err, ErrUnsupported)
		_, err = st.Put(ctx, bytes.NewReader(nil), PutOptions{})
		require.ErrorIs(t, err, ErrUnsupported)
	})
	t.Run("kind mismatch", func(t *testing.T) {
		_, err := st.Put(ctx, bytes.NewReader(pngBytes(t, 2, 2)), PutOptions{ExpectedKind: KindAudio})
		require.ErrorIs(t, err, ErrKindMismatch)
	})
	t.Run("svg disabled by default", func(t *testing.T) {
		_, err := st.Put(ctx, bytes.NewReader([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)), PutOptions{})
		require.ErrorIs(t, err, ErrUnsupported)
	})
	t.Run("reader error", func(t *testing.T) {
		boom := errors.New("boom")
		r := io.MultiReader(bytes.NewReader(pngBytes(t, 2, 2)), errReader{boom})
		_, err := st.Put(ctx, r, PutOptions{})
		require.ErrorIs(t, err, boom)
	})
	require.Equal(t, 0, rowCount(t, st))
	require.Empty(t, tmpEntries(t, st))
	entries, err := os.ReadDir(st.Dir())
	require.NoError(t, err)
	require.Len(t, entries, 1, "only tmp/ exists")
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

func TestPutSVGAllowed(t *testing.T) {
	limits := DefaultLimits()
	limits.AllowSVG = true
	st, _ := newTestStore(t, limits, nil)
	m, err := st.Put(context.Background(), bytes.NewReader([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)), PutOptions{})
	require.NoError(t, err)
	require.Equal(t, KindImage, m.Kind)
	require.Equal(t, "image/svg+xml", m.MIME)
	require.Equal(t, "svg", m.Ext)
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func TestPutLimits(t *testing.T) {
	limits := Limits{MaxImage: 1024, MaxAudio: 64 << 10, MaxVideo: 0, MaxHTML: 10}
	st, _ := newTestStore(t, limits, nil)
	ctx := context.Background()

	t.Run("image over limit aborts streaming", func(t *testing.T) {
		// 1 MiB of image bytes behind a valid PNG header; only ~limit+buffer may be read.
		img := append(pngBytes(t, 2, 2), make([]byte, 1<<20)...)
		cr := &countingReader{r: bytes.NewReader(img)}
		_, err := st.Put(ctx, cr, PutOptions{})
		require.ErrorIs(t, err, ErrTooLarge)
		require.Less(t, cr.n, int64(len(img)), "must not read the whole body")
		require.LessOrEqual(t, cr.n, int64(SniffLen+1024+64<<10))
		require.Empty(t, tmpEntries(t, st), "temp file removed")
	})
	t.Run("html limit smaller than sniff window", func(t *testing.T) {
		_, err := st.Put(ctx, bytes.NewReader([]byte("<!DOCTYPE html><p>too long</p>")), PutOptions{})
		require.ErrorIs(t, err, ErrTooLarge)
		require.Empty(t, tmpEntries(t, st))
	})
	t.Run("exactly at limit is accepted", func(t *testing.T) {
		data := mp3Frame()
		data = append(data, make([]byte, 64<<10-len(data))...)
		m, err := st.Put(ctx, bytes.NewReader(data), PutOptions{})
		require.NoError(t, err)
		require.Equal(t, int64(64<<10), m.Size)
	})
	t.Run("one byte over is rejected", func(t *testing.T) {
		data := mp3Frame()
		data = append(data, make([]byte, 64<<10-len(data)+1)...)
		_, err := st.Put(ctx, bytes.NewReader(data), PutOptions{})
		require.ErrorIs(t, err, ErrTooLarge)
	})
	t.Run("zero limit means unlimited", func(t *testing.T) {
		data := append(ebmlHeader("webm"), make([]byte, 3<<20)...)
		m, err := st.Put(ctx, bytes.NewReader(data), PutOptions{})
		require.NoError(t, err)
		require.Equal(t, int64(len(data)), m.Size)
	})
	require.Empty(t, tmpEntries(t, st))
	require.Equal(t, 2, rowCount(t, st))
}

func TestOpenAndStat(t *testing.T) {
	st, _ := newTestStore(t, DefaultLimits(), nil)
	ctx := context.Background()
	data := gifBytes(t, 8, 8)
	m, err := st.Put(ctx, bytes.NewReader(data), PutOptions{OriginalName: "anim.gif"})
	require.NoError(t, err)

	got, err := st.Stat(ctx, m.ID)
	require.NoError(t, err)
	require.Equal(t, m, got)

	rs, meta, err := st.Open(ctx, m.ID)
	require.NoError(t, err)
	require.Equal(t, m, meta)
	read, err := io.ReadAll(rs)
	require.NoError(t, err)
	require.Equal(t, data, read)
	_, err = rs.Seek(2, io.SeekStart)
	require.NoError(t, err)
	require.NoError(t, rs.Close())

	unknown := hex.EncodeToString(bytes.Repeat([]byte{1}, 32))
	_, err = st.Stat(ctx, unknown)
	require.ErrorIs(t, err, ErrNotFound)
	_, _, err = st.Open(ctx, unknown)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = st.Stat(ctx, "ABC")
	require.ErrorIs(t, err, ErrNotFound)
	_, err = st.Stat(ctx, "../../etc/passwd")
	require.ErrorIs(t, err, ErrNotFound)

	// Row without a file: reported as not found, not as a 500-style error.
	require.NoError(t, os.Remove(st.Path(m.ID)))
	_, _, err = st.Open(ctx, m.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestDelete(t *testing.T) {
	st, db := newTestStore(t, DefaultLimits(), nil)
	ctx := context.Background()
	m, err := st.Put(ctx, bytes.NewReader(pngBytes(t, 4, 4)), PutOptions{})
	require.NoError(t, err)
	path := st.Path(m.ID)

	_, err = db.Exec(`UPDATE media SET ref_count = 2 WHERE id = ?`, m.ID)
	require.NoError(t, err)
	err = st.Delete(ctx, m.ID)
	require.ErrorIs(t, err, ErrInUse)
	require.FileExists(t, path)

	_, err = db.Exec(`UPDATE media SET ref_count = 0 WHERE id = ?`, m.ID)
	require.NoError(t, err)
	require.NoError(t, st.Delete(ctx, m.ID))
	require.NoFileExists(t, path)
	_, err = st.Stat(ctx, m.ID)
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, st.Delete(ctx, m.ID), ErrNotFound)
	require.ErrorIs(t, st.Delete(ctx, "bogus"), ErrNotFound)

	// Missing file is tolerated.
	m2, err := st.Put(ctx, bytes.NewReader(jpegBytes(t, 4, 4)), PutOptions{})
	require.NoError(t, err)
	require.NoError(t, os.Remove(st.Path(m2.ID)))
	require.NoError(t, st.Delete(ctx, m2.ID))
	require.Equal(t, 0, rowCount(t, st))
}

func TestSweep(t *testing.T) {
	st, db := newTestStore(t, DefaultLimits(), nil)
	ctx := context.Background()
	old, err := st.Put(ctx, bytes.NewReader(pngBytes(t, 1, 1)), PutOptions{})
	require.NoError(t, err)
	oldRef, err := st.Put(ctx, bytes.NewReader(pngBytes(t, 2, 1)), PutOptions{})
	require.NoError(t, err)
	fresh, err := st.Put(ctx, bytes.NewReader(pngBytes(t, 3, 1)), PutOptions{})
	require.NoError(t, err)

	twoHoursAgo := time.Now().Add(-2 * time.Hour).UnixMilli()
	_, err = db.Exec(`UPDATE media SET created_at = ? WHERE id IN (?, ?)`, twoHoursAgo, old.ID, oldRef.ID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE media SET ref_count = 1 WHERE id = ?`, oldRef.ID)
	require.NoError(t, err)

	deleted, err := st.Sweep(ctx, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, deleted)
	_, err = st.Stat(ctx, old.ID)
	require.ErrorIs(t, err, ErrNotFound)
	require.NoFileExists(t, filepath.Join(st.Dir(), old.ID[:2], old.ID+".png"))
	_, err = st.Stat(ctx, oldRef.ID)
	require.NoError(t, err)
	_, err = st.Stat(ctx, fresh.ID)
	require.NoError(t, err)

	// Zero grace period sweeps everything unreferenced.
	deleted, err = st.Sweep(ctx, -time.Second)
	require.NoError(t, err)
	require.Equal(t, 1, deleted)
	require.Equal(t, 1, rowCount(t, st))

	deleted, err = st.Sweep(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, 0, deleted)
}

func TestPutProber(t *testing.T) {
	ctx := context.Background()

	t.Run("fake prober fills audio/video info", func(t *testing.T) {
		fp := &fakeProber{info: Info{DurationMs: 1234, Width: 640, Height: 360, Codec: "fake"}}
		st, _ := newTestStore(t, DefaultLimits(), fp)
		m, err := st.Put(ctx, bytes.NewReader(mp3Frame()), PutOptions{})
		require.NoError(t, err)
		require.Equal(t, int64(1234), m.DurationMs)
		require.Equal(t, 640, m.Width)
		require.Equal(t, 360, m.Height)
		calls := fp.calls()
		require.Len(t, calls, 1)
		require.True(t, filepath.IsAbs(calls[0]) || filepath.HasPrefix(calls[0], st.Dir()))
		require.Contains(t, calls[0], filepath.Join(st.Dir(), "tmp"), "probed before the move")

		got, err := st.Stat(ctx, m.ID)
		require.NoError(t, err)
		require.Equal(t, m, got, "persisted")
	})
	t.Run("images use stdlib, prober not called", func(t *testing.T) {
		fp := &fakeProber{info: Info{DurationMs: 40, Width: 1, Height: 1}}
		st, _ := newTestStore(t, DefaultLimits(), fp)
		m, err := st.Put(ctx, bytes.NewReader(pngBytes(t, 20, 10)), PutOptions{})
		require.NoError(t, err)
		require.Equal(t, 20, m.Width)
		require.Equal(t, 10, m.Height)
		require.Empty(t, fp.calls())

		_, err = st.Put(ctx, bytes.NewReader([]byte("<html><body>x</body></html>")), PutOptions{})
		require.NoError(t, err)
		require.Empty(t, fp.calls(), "html is never probed")
	})
	t.Run("prober fallback for images stdlib cannot decode", func(t *testing.T) {
		fp := &fakeProber{info: Info{DurationMs: 40, Width: 50, Height: 60, Codec: "bmp"}}
		st, _ := newTestStore(t, DefaultLimits(), fp)
		m, err := st.Put(ctx, bytes.NewReader(bmpHeader()), PutOptions{})
		require.NoError(t, err)
		require.Equal(t, 50, m.Width)
		require.Equal(t, 60, m.Height)
		require.Equal(t, int64(0), m.DurationMs, "still images have no duration")
		require.Len(t, fp.calls(), 1)
	})
	t.Run("prober failure is ignored", func(t *testing.T) {
		fp := &fakeProber{err: errors.New("ffprobe exploded")}
		st, _ := newTestStore(t, DefaultLimits(), fp)
		m, err := st.Put(ctx, bytes.NewReader(wavBytes(0.01, 8000)), PutOptions{})
		require.NoError(t, err)
		require.Equal(t, int64(0), m.DurationMs)
		require.FileExists(t, st.Path(m.ID))
	})
	t.Run("nop prober", func(t *testing.T) {
		st, _ := newTestStore(t, DefaultLimits(), NopProber{})
		m, err := st.Put(ctx, bytes.NewReader(wavBytes(0.01, 8000)), PutOptions{})
		require.NoError(t, err)
		require.Equal(t, int64(0), m.DurationMs)
	})
}

func TestPutConcurrentSameContent(t *testing.T) {
	st, _ := newTestStore(t, DefaultLimits(), nil)
	rng := rand.New(rand.NewSource(42))
	data := append(pngBytes(t, 5, 5), make([]byte, 200<<10)...)
	rng.Read(data[len(data)-200<<10:])

	const workers = 8
	var wg sync.WaitGroup
	ids := make([]string, workers)
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m, err := st.Put(context.Background(), bytes.NewReader(data), PutOptions{})
			ids[i], errs[i] = m.ID, err
		}(i)
	}
	wg.Wait()
	for i := range ids {
		require.NoError(t, errs[i])
		require.Equal(t, ids[0], ids[i])
	}
	require.Equal(t, 1, rowCount(t, st))
	require.Empty(t, tmpEntries(t, st))
}

func TestPutContextCancelled(t *testing.T) {
	st, _ := newTestStore(t, DefaultLimits(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := st.Put(ctx, bytes.NewReader(pngBytes(t, 2, 2)), PutOptions{})
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, tmpEntries(t, st))
	require.Equal(t, 0, rowCount(t, st))
}

func TestCleanOriginalName(t *testing.T) {
	require.Equal(t, "a.png", cleanOriginalName("dir/sub/a.png"))
	require.Equal(t, "a.png", cleanOriginalName(`C:\dir\a.png`))
	require.Equal(t, "ab.png", cleanOriginalName("a\x00b.png\n"))
	require.Equal(t, "", cleanOriginalName("   "))
	long := bytes.Repeat([]byte("я"), 300)
	require.Len(t, []rune(cleanOriginalName(string(long))), maxOriginalName)
}

func TestLimits(t *testing.T) {
	l := DefaultLimits()
	require.Equal(t, int64(20<<20), l.MaxImage)
	require.Equal(t, int64(100<<20), l.MaxAudio)
	require.Equal(t, int64(512<<20), l.MaxVideo)
	require.Equal(t, int64(1<<20), l.MaxHTML)
	require.False(t, l.AllowSVG)
	require.Equal(t, l.MaxImage, l.Max(KindImage))
	require.Equal(t, l.MaxAudio, l.Max(KindAudio))
	require.Equal(t, l.MaxVideo, l.Max(KindVideo))
	require.Equal(t, l.MaxHTML, l.Max(KindHTML))
	require.Equal(t, int64(0), l.Max(Kind("other")))
}
