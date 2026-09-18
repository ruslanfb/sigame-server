// Package media stores uploaded/imported media files content-addressed by
// sha256 and serves them over HTTP with Range support. Metadata lives in SQLite;
// bytes live under <dataDir>/media/<first two hex>/<sha256>.<ext>.
package media

import (
	"context"
	"errors"
	"io"
)

// Kind is the coarse media category (matches packs.ContentType for non-text items).
type Kind string

const (
	KindImage Kind = "image"
	KindAudio Kind = "audio"
	KindVideo Kind = "video"
	KindHTML  Kind = "html"
)

// Meta describes a stored media object.
type Meta struct {
	ID           string `json:"id" doc:"sha256 hex of the content"`
	Kind         Kind   `json:"kind" enum:"image,audio,video,html"`
	MIME         string `json:"mime" doc:"Sniffed content type, e.g. image/png"`
	Ext          string `json:"ext" doc:"File extension without dot, derived from MIME"`
	Size         int64  `json:"size" doc:"Bytes"`
	DurationMs   int64  `json:"durationMs,omitempty" doc:"Audio/video duration if known (ffprobe)"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	OriginalName string `json:"originalName,omitempty" doc:"File name at upload/import time"`
	RefCount     int    `json:"refCount" doc:"Number of packs referencing this media"`
	CreatedAt    int64  `json:"createdAt" doc:"Unix ms UTC"`
}

// PutOptions carries optional hints for Put.
type PutOptions struct {
	OriginalName string // used for the ext fallback and reporting
	ExpectedKind Kind   // "" = accept any allowed kind; otherwise reject mismatching content
}

// Sentinel errors.
var (
	ErrNotFound     = errors.New("media: not found")
	ErrUnsupported  = errors.New("media: unsupported content type")
	ErrTooLarge     = errors.New("media: file too large")
	ErrInUse        = errors.New("media: still referenced by a pack")
	ErrKindMismatch = errors.New("media: content kind does not match the expected kind")
)

// Store is the media repository used by the HTTP API, the .siq codec and rooms.
// Implementations must be safe for concurrent use.
type Store interface {
	// Put streams r into the store, computing sha256, sniffing the MIME type and
	// probing duration/dimensions. Identical content returns the existing Meta.
	Put(ctx context.Context, r io.Reader, opts PutOptions) (Meta, error)
	// Open returns a seekable reader for HTTP Range serving.
	Open(ctx context.Context, id string) (io.ReadSeekCloser, Meta, error)
	// Stat returns metadata only.
	Stat(ctx context.Context, id string) (Meta, error)
	// Delete removes an object; ErrInUse if RefCount > 0.
	Delete(ctx context.Context, id string) error
}

// Reference counts (Meta.RefCount) are maintained by the pack repository in
// its own SQL transaction via the pack_media table; the media store only reads
// them (Stat/Delete/GC).
