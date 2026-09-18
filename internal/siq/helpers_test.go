package siq

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"sigame/internal/media"
	"sigame/internal/packs"
)

// zipEntry describes one entry of an in-memory test archive.
type zipEntry struct {
	name    string
	data    []byte
	nonUTF8 bool // clear the UTF-8 flag (legacy cp866 names)
	stored  bool // zip.Store instead of Deflate
}

func buildZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate, NonUTF8: e.nonUTF8}
		if e.stored {
			h.Method = zip.Store
		}
		w, err := zw.CreateHeader(h)
		require.NoError(t, err)
		_, err = w.Write(e.data)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func importBytes(t *testing.T, data []byte, store media.Store, opts ImportOptions) (*packs.Pack, *Report, error) {
	t.Helper()
	return Import(context.Background(), bytes.NewReader(data), int64(len(data)), store, opts)
}

func fixtureDir() string { return filepath.Join("..", "..", "testdata", "siq") }

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir(), name))
	require.NoError(t, err)
	return data
}

func listFixtures(t *testing.T) []string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(fixtureDir(), "*.siq"))
	require.NoError(t, err)
	require.NotEmpty(t, names)
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, filepath.Base(n))
	}
	return out
}

// readZipEntry returns the bytes of one entry of an archive.
func readZipEntry(t *testing.T, data []byte, name string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			require.NoError(t, err)
			defer rc.Close()
			var buf bytes.Buffer
			_, err = buf.ReadFrom(rc)
			require.NoError(t, err)
			return buf.Bytes()
		}
	}
	t.Fatalf("entry %q not found", name)
	return nil
}

// normalize returns a deep copy of the pack with every ID, version and
// timestamp cleared so that two imports of the same content compare equal.
func normalize(t *testing.T, p *packs.Pack) *packs.Pack {
	t.Helper()
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	var c packs.Pack
	require.NoError(t, json.Unmarshal(raw, &c))
	c.ID, c.Version, c.CreatedAt, c.UpdatedAt, c.SIQID = "", 0, 0, 0, ""
	for ri := range c.Rounds {
		c.Rounds[ri].ID = ""
		for ti := range c.Rounds[ri].Themes {
			c.Rounds[ri].Themes[ti].ID = ""
			for qi := range c.Rounds[ri].Themes[ti].Questions {
				c.Rounds[ri].Themes[ti].Questions[qi].ID = ""
			}
		}
	}
	return &c
}

// findTheme returns the first theme with the given name.
func findTheme(t *testing.T, p *packs.Pack, name string) packs.Theme {
	t.Helper()
	for _, r := range p.Rounds {
		for _, th := range r.Themes {
			if th.Name == name {
				return th
			}
		}
	}
	t.Fatalf("theme %q not found", name)
	return packs.Theme{}
}

func questionByPrice(t *testing.T, th packs.Theme, price int) packs.Question {
	t.Helper()
	for _, q := range th.Questions {
		if q.Price == price {
			return q
		}
	}
	t.Fatalf("question with price %d not found in theme %q", price, th.Name)
	return packs.Question{}
}

func entriesWithLevel(r *Report, level string) []Entry {
	var out []Entry
	for _, e := range r.Entries {
		if e.Level == level {
			out = append(out, e)
		}
	}
	return out
}
