package siq

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"

	"sigame/internal/media"
)

// fakeStore is an in-memory media.Store: content-addressed by sha256, kind
// and extension derived from the original name, optional size cap and
// rejected extensions to simulate ErrTooLarge / ErrUnsupported.
type fakeStore struct {
	mu        sync.Mutex
	objects   map[string]*fakeObject
	puts      []media.PutOptions
	maxSize   int64
	rejectExt map[string]bool
}

type fakeObject struct {
	meta media.Meta
	data []byte
}

func newFakeStore() *fakeStore {
	return &fakeStore{objects: map[string]*fakeObject{}}
}

func kindByExt(ext string) media.Kind {
	switch ext {
	case "png", "jpg", "jpeg", "gif", "webp", "bmp":
		return media.KindImage
	case "mp3", "ogg", "wav", "m4a", "flac":
		return media.KindAudio
	case "mp4", "webm", "mkv", "avi":
		return media.KindVideo
	case "html", "htm":
		return media.KindHTML
	}
	return ""
}

func (s *fakeStore) Put(_ context.Context, r io.Reader, opts media.PutOptions) (media.Meta, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return media.Meta{}, fmt.Errorf("fake store: read: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.puts = append(s.puts, opts)
	if s.maxSize > 0 && int64(len(data)) > s.maxSize {
		return media.Meta{}, media.ErrTooLarge
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(opts.OriginalName), "."))
	if s.rejectExt[ext] {
		return media.Meta{}, media.ErrUnsupported
	}
	kind := opts.ExpectedKind
	if kind == "" {
		kind = kindByExt(ext)
	}
	if ext == "" {
		ext = map[media.Kind]string{media.KindImage: "png", media.KindAudio: "mp3", media.KindVideo: "mp4", media.KindHTML: "html"}[kind]
		if ext == "" {
			ext = "bin"
		}
	}
	sum := sha256.Sum256(data)
	id := hex.EncodeToString(sum[:])
	if o, ok := s.objects[id]; ok {
		return o.meta, nil
	}
	meta := media.Meta{ID: id, Kind: kind, MIME: "application/octet-stream", Ext: ext, Size: int64(len(data)), OriginalName: opts.OriginalName}
	s.objects[id] = &fakeObject{meta: meta, data: data}
	return meta, nil
}

type readSeekCloser struct {
	*bytes.Reader
}

func (readSeekCloser) Close() error { return nil }

func (s *fakeStore) Open(_ context.Context, id string) (io.ReadSeekCloser, media.Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.objects[id]
	if !ok {
		return nil, media.Meta{}, media.ErrNotFound
	}
	return readSeekCloser{bytes.NewReader(o.data)}, o.meta, nil
}

func (s *fakeStore) Stat(_ context.Context, id string) (media.Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.objects[id]
	if !ok {
		return media.Meta{}, media.ErrNotFound
	}
	return o.meta, nil
}

func (s *fakeStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.objects[id]; !ok {
		return media.ErrNotFound
	}
	delete(s.objects, id)
	return nil
}

// add stores data directly (for export tests) and returns its media ID.
func (s *fakeStore) add(name string, data []byte, kind media.Kind) string {
	meta, err := s.Put(context.Background(), bytes.NewReader(data), media.PutOptions{OriginalName: name, ExpectedKind: kind})
	if err != nil {
		panic(err)
	}
	return meta.ID
}
