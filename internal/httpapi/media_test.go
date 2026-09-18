package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sigame/internal/media"
	"sigame/internal/packs"
)

func TestMediaUploadGetDelete(t *testing.T) {
	e := newTestEnv(t)

	data := pngBytes(t, 32, 16)
	body, ct := multipartBody(t, "file", "картинка.png", data)
	rec := e.do(t, http.MethodPost, "/api/v1/media", body, "Content-Type", ct)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var m media.Meta
	decodeJSON(t, rec, &m)
	require.Len(t, m.ID, 64)
	require.Equal(t, media.KindImage, m.Kind)
	require.Equal(t, "image/png", m.MIME)
	require.Equal(t, int64(len(data)), m.Size)
	require.Equal(t, 32, m.Width)
	require.Equal(t, 16, m.Height)
	require.Equal(t, "картинка.png", m.OriginalName)
	require.Equal(t, "/api/v1/media/"+m.ID, rec.Header().Get("Location"))

	// Same content again → 200 with the same id. The dedup status is decided
	// by comparing createdAt (ms) with the request start, so leave a tick.
	time.Sleep(3 * time.Millisecond)
	body, ct = multipartBody(t, "file", "other.png", data)
	rec = e.do(t, http.MethodPost, "/api/v1/media?kind=image", body, "Content-Type", ct)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var again media.Meta
	decodeJSON(t, rec, &again)
	require.Equal(t, m.ID, again.ID)

	// Metadata.
	rec = e.do(t, http.MethodGet, "/api/v1/media/"+m.ID, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	decodeJSON(t, rec, &again)
	require.Equal(t, m.ID, again.ID)
	require.Equal(t, 0, again.RefCount)

	// Streaming with Range → 206.
	rec = e.do(t, http.MethodGet, "/media/"+m.ID, nil, "Range", "bytes=0-9")
	require.Equal(t, http.StatusPartialContent, rec.Code)
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	require.Contains(t, rec.Header().Get("Content-Range"), "bytes 0-9/")
	require.Equal(t, data[:10], rec.Body.Bytes())
	require.Equal(t, `"`+m.ID+`"`, rec.Header().Get("ETag"))
	rec = e.do(t, http.MethodGet, "/media/"+m.ID, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, data, rec.Body.Bytes())
	rec = e.do(t, http.MethodGet, "/media/"+m.ID, nil, "If-None-Match", `"`+m.ID+`"`)
	require.Equal(t, http.StatusNotModified, rec.Code)
	rec = e.do(t, http.MethodHead, "/media/"+m.ID, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	rec = e.do(t, http.MethodGet, "/media/deadbeef", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)

	// Referenced by a pack → delete is refused with 409.
	pk := samplePack("With image")
	pk.Rounds[0].Themes[0].Questions[0].Params.Question = append(pk.Rounds[0].Themes[0].Questions[0].Params.Question,
		packs.ContentItem{Type: packs.ContentImage, MediaID: m.ID})
	stored := e.createPack(t, pk)
	rec = e.do(t, http.MethodGet, "/api/v1/media/"+m.ID, nil)
	decodeJSON(t, rec, &again)
	require.Equal(t, 1, again.RefCount)
	rec = e.do(t, http.MethodDelete, "/api/v1/media/"+m.ID, nil)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	require.Equal(t, http.StatusConflict, decodeProblem(t, rec).Status)

	// After the pack is gone the object can be deleted.
	rec = e.do(t, http.MethodDelete, "/api/v1/packs/"+stored.ID, nil)
	require.Equal(t, http.StatusNoContent, rec.Code)
	rec = e.do(t, http.MethodDelete, "/api/v1/media/"+m.ID, nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	rec = e.do(t, http.MethodGet, "/api/v1/media/"+m.ID, nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = e.do(t, http.MethodDelete, "/api/v1/media/"+m.ID, nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = e.do(t, http.MethodGet, "/media/"+m.ID, nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestMediaUploadRejections(t *testing.T) {
	e := newTestEnv(t, withLimits(media.Limits{MaxImage: 1024, MaxAudio: 1024, MaxVideo: 1024, MaxHTML: 1024}))

	// Unsupported content → 415.
	body, ct := multipartBody(t, "file", "blob.bin", []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09})
	rec := e.do(t, http.MethodPost, "/api/v1/media", body, "Content-Type", ct)
	require.Equal(t, http.StatusUnsupportedMediaType, rec.Code, rec.Body.String())
	require.Equal(t, http.StatusUnsupportedMediaType, decodeProblem(t, rec).Status)

	// Kind mismatch → 415.
	small := pngBytes(t, 4, 4)
	require.Less(t, len(small), 1024)
	body, ct = multipartBody(t, "file", "pic.png", small)
	rec = e.do(t, http.MethodPost, "/api/v1/media?kind=audio", body, "Content-Type", ct)
	require.Equal(t, http.StatusUnsupportedMediaType, rec.Code, rec.Body.String())

	// Invalid kind → 422.
	body, ct = multipartBody(t, "file", "pic.png", small)
	rec = e.do(t, http.MethodPost, "/api/v1/media?kind=movie", body, "Content-Type", ct)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Equal(t, "query.kind", decodeProblem(t, rec).Errors[0].Location)

	// Oversize for its kind → 413.
	big := pngBytes(t, 200, 200)
	require.Greater(t, len(big), 1024)
	body, ct = multipartBody(t, "file", "big.png", big)
	rec = e.do(t, http.MethodPost, "/api/v1/media", body, "Content-Type", ct)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())

	// Missing file part → 422, non-multipart → 415.
	body, ct = multipartBody(t, "picture", "pic.png", small)
	rec = e.do(t, http.MethodPost, "/api/v1/media", body, "Content-Type", ct)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	rec = e.do(t, http.MethodPost, "/api/v1/media", []byte("raw"))
	require.Equal(t, http.StatusUnsupportedMediaType, rec.Code, rec.Body.String())
}
