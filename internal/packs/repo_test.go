package packs

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"sigame/internal/db"
)

// newTestRepo opens an in-memory database with a deterministic clock that
// advances by one second per call.
func newTestRepo(t *testing.T) (*Repo, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	sqlDB, err := db.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(ctx, sqlDB))
	r := NewRepo(sqlDB)
	var clock int64 = 1_700_000_000_000
	r.now = func() int64 { clock += 1000; return clock }
	return r, sqlDB
}

func insertMedia(t *testing.T, sqlDB *sql.DB, ids ...string) {
	t.Helper()
	for _, id := range ids {
		_, err := sqlDB.ExecContext(context.Background(),
			`INSERT INTO media (id, kind, mime, ext, size, created_at) VALUES (?, 'image', 'image/png', 'png', 1, 1)`, id)
		require.NoError(t, err)
	}
}

func refCount(t *testing.T, sqlDB *sql.DB, id string) int {
	t.Helper()
	var n int
	require.NoError(t, sqlDB.QueryRowContext(context.Background(), `SELECT ref_count FROM media WHERE id = ?`, id).Scan(&n))
	return n
}

func packMediaRows(t *testing.T, sqlDB *sql.DB, packID string) int {
	t.Helper()
	var n int
	require.NoError(t, sqlDB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM pack_media WHERE pack_id = ?`, packID).Scan(&n))
	return n
}

func TestRepo_CreateGetRoundTrip(t *testing.T) {
	r, sqlDB := newTestRepo(t)
	ctx := context.Background()
	insertMedia(t, sqlDB, mediaA, mediaB)

	p := samplePack()
	require.NoError(t, r.Create(ctx, p))
	require.NotEmpty(t, p.ID)
	require.Equal(t, 1, p.Version)
	require.Equal(t, int64(1_700_000_001_000), p.CreatedAt)
	require.Equal(t, p.CreatedAt, p.UpdatedAt)
	require.NotEmpty(t, p.Rounds[0].Themes[0].Questions[0].ID)

	got, err := r.Get(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, p, got, "stored pack must round-trip exactly")

	// The stored document is the JSON of the normalised pack.
	var doc string
	require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT doc FROM packs WHERE id = ?`, p.ID).Scan(&doc))
	var fromDoc Pack
	require.NoError(t, json.Unmarshal([]byte(doc), &fromDoc))
	require.Equal(t, *p, fromDoc)

	s, err := r.GetSummary(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, Summarize(p), s)
	require.Equal(t, "12+", s.Restriction, "restriction is read from the JSON document")

	_, err = r.Get(ctx, "missing")
	require.ErrorIs(t, err, ErrNotFound)
	_, err = r.GetSummary(ctx, "missing")
	require.ErrorIs(t, err, ErrNotFound)

	require.ErrorIs(t, r.Create(ctx, p), ErrAlreadyExists)
}

func TestRepo_Create_Validation(t *testing.T) {
	r, sqlDB := newTestRepo(t)
	ctx := context.Background()

	bad := samplePack()
	bad.Name = ""
	err := r.Create(ctx, bad)
	requireProblem(t, err, "/name", CodeRequired)

	// Media referenced by the pack must exist; nothing is written otherwise.
	insertMedia(t, sqlDB, mediaA)
	p := samplePack()
	err = r.Create(ctx, p)
	requireProblem(t, err, "/rounds/0/themes/0/questions/1/params/question/1/mediaId", CodeMediaNotFound)
	ps := problems(t, err)
	require.Len(t, ps, 1)
	res, err := r.List(ctx, ListFilter{})
	require.NoError(t, err)
	require.Equal(t, 0, res.Total)
	require.Equal(t, 0, refCount(t, sqlDB, mediaA), "rolled back: no ref count change")

	require.Error(t, r.Create(ctx, nil))
}

func TestRepo_Update_VersionConflict(t *testing.T) {
	r, sqlDB := newTestRepo(t)
	ctx := context.Background()
	insertMedia(t, sqlDB, mediaA, mediaB)

	p := samplePack()
	require.NoError(t, r.Create(ctx, p))
	createdAt := p.CreatedAt

	p.Name = "Renamed"
	require.ErrorIs(t, r.Update(ctx, p, 999), ErrVersionConflict)
	got, err := r.Get(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, "Sample pack", got.Name, "conflict must not write")

	require.NoError(t, r.Update(ctx, p, 1))
	require.Equal(t, 2, p.Version)
	require.Equal(t, createdAt, p.CreatedAt)
	require.Greater(t, p.UpdatedAt, p.CreatedAt)
	got, err = r.Get(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, p, got)

	// A stale writer loses.
	stale := samplePack()
	stale.ID = p.ID
	stale.Name = "Stale"
	require.ErrorIs(t, r.Update(ctx, stale, 1), ErrVersionConflict)

	// expectedVersion 0 skips the check; client-sent version/createdAt are ignored.
	stale.Version = 77
	stale.CreatedAt = 5
	stale.Difficulty = 0
	require.NoError(t, r.Update(ctx, stale, 0))
	require.Equal(t, 3, stale.Version)
	require.Equal(t, createdAt, stale.CreatedAt)
	require.Equal(t, 0, stale.Difficulty, "explicit 0 on an existing pack is kept")

	// Validation and not-found paths.
	stale.Name = ""
	requireProblem(t, r.Update(ctx, stale, 0), "/name", CodeRequired)
	missing := samplePack()
	missing.ID = NewID()
	require.ErrorIs(t, r.Update(ctx, missing, 0), ErrNotFound)
	require.ErrorIs(t, r.Update(ctx, &Pack{}, 0), ErrNotFound)
}

func TestRepo_MediaRefCounts(t *testing.T) {
	r, sqlDB := newTestRepo(t)
	ctx := context.Background()
	insertMedia(t, sqlDB, mediaA, mediaB, mediaC)

	p := samplePack() // references A and B
	require.NoError(t, r.Create(ctx, p))
	require.Equal(t, 1, refCount(t, sqlDB, mediaA))
	require.Equal(t, 1, refCount(t, sqlDB, mediaB))
	require.Equal(t, 0, refCount(t, sqlDB, mediaC))
	require.Equal(t, 2, packMediaRows(t, sqlDB, p.ID))
	refs, err := r.MediaRefs(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, []string{mediaA, mediaB}, refs)

	// Replace B with C (and reference C twice: only one ref row/count).
	q(p, 1).Params.Question[1] = ContentItem{Type: ContentAudio, MediaID: mediaC}
	q(p, 0).Params.Answer = []ContentItem{{Type: ContentImage, MediaID: mediaC}}
	require.NoError(t, r.Update(ctx, p, p.Version))
	require.Equal(t, 1, refCount(t, sqlDB, mediaA))
	require.Equal(t, 0, refCount(t, sqlDB, mediaB))
	require.Equal(t, 1, refCount(t, sqlDB, mediaC))
	refs, err = r.MediaRefs(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, []string{mediaA, mediaC}, refs)

	// An update that adds unknown media fails atomically.
	broken := samplePack()
	broken.ID = p.ID
	broken.LogoMediaID = hex64('d')
	err = r.Update(ctx, broken, 0)
	requireProblem(t, err, "/logoMediaId", CodeMediaNotFound)
	require.Equal(t, 1, refCount(t, sqlDB, mediaC), "failed update leaves counts untouched")
	require.Equal(t, 0, refCount(t, sqlDB, mediaB))
	got, err := r.Get(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, 2, got.Version)

	// A second pack sharing A.
	p2 := samplePack()
	p2.Name = "Second"
	require.NoError(t, r.Create(ctx, p2))
	require.Equal(t, 2, refCount(t, sqlDB, mediaA))
	require.Equal(t, 1, refCount(t, sqlDB, mediaB))

	// Deleting releases only the deleted pack's references.
	require.NoError(t, r.Delete(ctx, p.ID))
	require.Equal(t, 1, refCount(t, sqlDB, mediaA))
	require.Equal(t, 1, refCount(t, sqlDB, mediaB))
	require.Equal(t, 0, refCount(t, sqlDB, mediaC))
	require.Equal(t, 0, packMediaRows(t, sqlDB, p.ID))
	_, err = r.Get(ctx, p.ID)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = r.MediaRefs(ctx, p.ID)
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, r.Delete(ctx, p.ID), ErrNotFound)

	require.NoError(t, r.Delete(ctx, p2.ID))
	require.Equal(t, 0, refCount(t, sqlDB, mediaA))
	require.Equal(t, 0, refCount(t, sqlDB, mediaB))
}

func TestRepo_Duplicate(t *testing.T) {
	r, sqlDB := newTestRepo(t)
	ctx := context.Background()
	insertMedia(t, sqlDB, mediaA, mediaB)

	src := samplePack()
	src.SIQID = "siq-123"
	require.NoError(t, r.Create(ctx, src))

	cp, err := r.Duplicate(ctx, src.ID, "")
	require.NoError(t, err)
	require.Equal(t, "Sample pack (copy)", cp.Name)
	require.NotEqual(t, src.ID, cp.ID)
	require.Equal(t, 1, cp.Version)
	require.Greater(t, cp.CreatedAt, src.CreatedAt)
	require.Empty(t, cp.SIQID)
	require.Equal(t, 2, refCount(t, sqlDB, mediaA))

	require.Len(t, cp.Rounds, len(src.Rounds))
	for i := range src.Rounds {
		require.NotEqual(t, src.Rounds[i].ID, cp.Rounds[i].ID)
		require.Equal(t, src.Rounds[i].Name, cp.Rounds[i].Name)
		for j := range src.Rounds[i].Themes {
			require.NotEqual(t, src.Rounds[i].Themes[j].ID, cp.Rounds[i].Themes[j].ID)
			for k := range src.Rounds[i].Themes[j].Questions {
				sq, cq := src.Rounds[i].Themes[j].Questions[k], cp.Rounds[i].Themes[j].Questions[k]
				require.NotEqual(t, sq.ID, cq.ID)
				require.Equal(t, sq.Params, cq.Params)
				require.Equal(t, sq.Right, cq.Right)
			}
		}
	}

	// The copy is independent of the source.
	stored, err := r.Get(ctx, cp.ID)
	require.NoError(t, err)
	require.Equal(t, cp, stored)
	q(cp, 1).Params.Price.Min = 999
	require.Equal(t, 100, q(src, 1).Params.Price.Min)

	named, err := r.Duplicate(ctx, src.ID, "  Named copy ")
	require.NoError(t, err)
	require.Equal(t, "Named copy", named.Name)

	_, err = r.Duplicate(ctx, "missing", "x")
	require.ErrorIs(t, err, ErrNotFound)

	res, err := r.List(ctx, ListFilter{})
	require.NoError(t, err)
	require.Equal(t, 3, res.Total)
}

func TestRepo_List(t *testing.T) {
	r, sqlDB := newTestRepo(t)
	ctx := context.Background()
	insertMedia(t, sqlDB, mediaA, mediaB)

	textOnly := func(name string) *Pack {
		return &Pack{Name: name, Rounds: []Round{{Themes: []Theme{{Questions: []Question{
			{Price: 100, Params: QuestionParams{Question: []ContentItem{{Type: ContentText, Text: "t"}}}},
		}}}}}}
	}

	// Created in this order; the fake clock makes createdAt strictly increasing.
	p1 := samplePack() // "Sample pack", ru-RU, difficulty 3, tags history/Science, media, authors Alice/Bob
	require.NoError(t, r.Create(ctx, p1))
	p2 := textOnly("Своя Игра: История")
	p2.Language = "ru-RU"
	p2.Difficulty = 8
	p2.Tags = []string{"История", "Кино"}
	p2.Info.Authors = []string{"Владимир"}
	require.NoError(t, r.Create(ctx, p2))
	p3 := textOnly("alpha quiz")
	p3.Language = "en-US"
	p3.Difficulty = 5
	p3.Tags = []string{"science"}
	require.NoError(t, r.Create(ctx, p3))
	p4 := textOnly("Beta quiz")
	p4.Language = "en-US"
	p4.Difficulty = 1
	require.NoError(t, r.Create(ctx, p4))

	names := func(res ListResult) []string {
		out := make([]string, 0, len(res.Items))
		for _, s := range res.Items {
			out = append(out, s.Name)
		}
		return out
	}
	list := func(f ListFilter) ListResult {
		t.Helper()
		res, err := r.List(ctx, f)
		require.NoError(t, err)
		return res
	}

	// Default: newest updated first.
	res := list(ListFilter{})
	require.Equal(t, 4, res.Total)
	require.Equal(t, []string{"Beta quiz", "alpha quiz", "Своя Игра: История", "Sample pack"}, names(res))
	require.Equal(t, []string{"history", "Science"}, res.Items[3].Tags)
	require.Equal(t, []string{"Alice", "Bob"}, res.Items[3].Authors)
	require.Equal(t, "12+", res.Items[3].Restriction)
	require.True(t, res.Items[3].HasMedia)
	require.Equal(t, []string{}, res.Items[0].Tags, "empty tag list is [] not nil")

	// Query: Unicode case-insensitive on name, authors and tags.
	require.Equal(t, []string{"Своя Игра: История"}, names(list(ListFilter{Query: "история"})))
	require.Equal(t, []string{"Своя Игра: История"}, names(list(ListFilter{Query: "владимир"})))
	require.Equal(t, []string{"Sample pack"}, names(list(ListFilter{Query: "alice"})))
	require.Equal(t, []string{"alpha quiz", "Sample pack"}, names(list(ListFilter{Query: "SCIENCE", Sort: SortName})))
	require.Equal(t, []string{"Beta quiz", "alpha quiz"}, names(list(ListFilter{Query: "quiz"})))
	require.Equal(t, 0, list(ListFilter{Query: "%"}).Total, "LIKE wildcards are escaped")
	require.Equal(t, 0, list(ListFilter{Query: "_"}).Total)

	// Language, tag (exact, case-insensitive), difficulty range, media.
	require.Equal(t, []string{"Beta quiz", "alpha quiz"}, names(list(ListFilter{Language: "en-US"})))
	require.Equal(t, []string{"Своя Игра: История"}, names(list(ListFilter{Tag: "история"})))
	require.Equal(t, 0, list(ListFilter{Tag: "истор"}).Total, "tag filter is an exact match")
	require.Equal(t, []string{"alpha quiz", "Sample pack"}, names(list(ListFilter{Tag: "SCIENCE", Sort: SortName})))
	require.Equal(t, []string{"alpha quiz", "Sample pack"}, names(list(ListFilter{MinDifficulty: 3, MaxDifficulty: 5, Sort: SortName})))
	require.Equal(t, []string{"Своя Игра: История"}, names(list(ListFilter{MinDifficulty: 6})))
	require.Equal(t, []string{"Beta quiz"}, names(list(ListFilter{MaxDifficulty: 2})))
	yes, no := true, false
	require.Equal(t, []string{"Sample pack"}, names(list(ListFilter{HasMedia: &yes})))
	require.Equal(t, 3, list(ListFilter{HasMedia: &no}).Total)

	// Sorting.
	require.Equal(t, []string{"alpha quiz", "Beta quiz", "Sample pack", "Своя Игра: История"}, names(list(ListFilter{Sort: SortName})))
	require.Equal(t, []string{"Своя Игра: История", "Sample pack", "Beta quiz", "alpha quiz"}, names(list(ListFilter{Sort: SortName, Order: "desc"})))
	require.Equal(t, []string{"Sample pack", "Своя Игра: История", "alpha quiz", "Beta quiz"}, names(list(ListFilter{Sort: SortCreatedAt, Order: "asc"})))
	require.NoError(t, r.Update(ctx, p1, 0))
	require.Equal(t, "Sample pack", list(ListFilter{Sort: SortUpdatedAt}).Items[0].Name, "update moves the pack to the top")
	require.Equal(t, "Sample pack", list(ListFilter{Sort: SortCreatedAt, Order: "asc"}).Items[0].Name, "createdAt is unchanged")
	_, err := r.List(ctx, ListFilter{Sort: "bogus"})
	require.Error(t, err)
	_, err = r.List(ctx, ListFilter{Order: "sideways"})
	require.Error(t, err)

	// Paging keeps the total.
	page := list(ListFilter{Sort: SortName, Limit: 2})
	require.Equal(t, 4, page.Total)
	require.Equal(t, []string{"alpha quiz", "Beta quiz"}, names(page))
	page = list(ListFilter{Sort: SortName, Limit: 2, Offset: 2})
	require.Equal(t, 4, page.Total)
	require.Equal(t, []string{"Sample pack", "Своя Игра: История"}, names(page))
	page = list(ListFilter{Sort: SortName, Limit: 2, Offset: 10})
	require.Equal(t, 4, page.Total)
	require.Equal(t, []string{}, names(page))
	require.NotNil(t, page.Items)

	// Limit is capped and defaulted.
	require.Len(t, list(ListFilter{Limit: 100_000}).Items, 4)
	require.Len(t, list(ListFilter{Limit: -1, Offset: -1}).Items, 4)
}

func TestRepo_ListLimitCap(t *testing.T) {
	r, _ := newTestRepo(t)
	ctx := context.Background()
	for i := 0; i < MaxListLimit+5; i++ {
		require.NoError(t, r.Create(ctx, &Pack{Name: "p"}))
	}
	res, err := r.List(ctx, ListFilter{Limit: MaxListLimit + 100})
	require.NoError(t, err)
	require.Equal(t, MaxListLimit+5, res.Total)
	require.Len(t, res.Items, MaxListLimit)
	res, err = r.List(ctx, ListFilter{})
	require.NoError(t, err)
	require.Len(t, res.Items, DefaultListLimit)
}

func TestDiffRefsAndEscapeLike(t *testing.T) {
	added, removed := diffRefs([]string{"a", "b", "c"}, []string{"b", "c", "d", "d"})
	require.Equal(t, []string{"d"}, added)
	require.Equal(t, []string{"a"}, removed)
	added, removed = diffRefs(nil, nil)
	require.Empty(t, added)
	require.Empty(t, removed)
	require.Equal(t, `a\%b\_c\\d`, escapeLike(`a%b_c\d`))
}
