package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"sigame/internal/media"
)

type mediaIDInput struct {
	ID string `path:"id" doc:"Media ID (sha256 hex of the content)"`
}

type mediaMetaOutput struct {
	Body media.Meta
}

func (s *Server) registerMedia() {
	huma.Register(s.API, huma.Operation{
		OperationID: "getMediaMeta",
		Method:      http.MethodGet,
		Path:        basePath + "/media/{id}",
		Tags:        []string{tagMedia},
		Summary:     "Get media metadata",
		Description: "Returns the stored metadata of a media object (kind, MIME type, size, dimensions/duration when known, reference count). The bytes are served by GET /media/{id}.",
		Errors:      []int{http.StatusNotFound},
	}, s.getMediaMeta)

	huma.Register(s.API, huma.Operation{
		OperationID: "deleteMedia",
		Method:      http.MethodDelete,
		Path:        basePath + "/media/{id}",
		Tags:        []string{tagMedia},
		Summary:     "Delete a media object",
		Description: "Removes the object and its bytes. Fails with 409 while any pack still references it.",
		Errors:      []int{http.StatusNotFound, http.StatusConflict},
	}, s.deleteMedia)
}

func (s *Server) getMediaMeta(ctx context.Context, in *mediaIDInput) (*mediaMetaOutput, error) {
	m, err := s.deps.Media.Stat(ctx, in.ID)
	if err != nil {
		return nil, s.fail(err)
	}
	return &mediaMetaOutput{Body: m}, nil
}

func (s *Server) deleteMedia(ctx context.Context, in *mediaIDInput) (*struct{}, error) {
	if err := s.deps.Media.Delete(ctx, in.ID); err != nil {
		return nil, s.fail(err)
	}
	return nil, nil
}

// uploadMedia handles POST /api/v1/media (multipart/form-data, field "file",
// optional query kind=image|audio|video|html). The part is streamed straight
// into the store, which sniffs the type, enforces the per-kind size limit
// and de-duplicates by sha256. A new object answers 201, an already stored
// one 200 (decided by comparing the object's createdAt with the request
// start).
func (s *Server) uploadMedia(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	kind := media.Kind(r.URL.Query().Get("kind"))
	switch kind {
	case "", media.KindImage, media.KindAudio, media.KindVideo, media.KindHTML:
	default:
		writeProblem(w, huma.Error422UnprocessableEntity("invalid kind",
			&huma.ErrorDetail{Location: "query.kind", Message: "expected one of image, audio, video, html", Value: string(kind)}))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.mediaBodyCap)
	part, err := openFilePart(r, "file")
	if err != nil {
		writeProblem(w, err)
		return
	}
	defer part.Close()

	start := time.Now().UnixMilli()
	meta, err := s.deps.Media.Put(ctx, part, media.PutOptions{OriginalName: part.FileName(), ExpectedKind: kind})
	if err != nil {
		writeProblem(w, s.fail(err))
		return
	}
	status := http.StatusOK
	if meta.CreatedAt >= start {
		status = http.StatusCreated
	}
	w.Header().Set("Location", basePath+"/media/"+meta.ID)
	writeJSON(w, status, meta)
}
