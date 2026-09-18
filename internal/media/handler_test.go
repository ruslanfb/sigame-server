package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

type handlerFixture struct {
	srv   *httptest.Server
	store *DiskStore
	png   Meta
	html  Meta
	data  []byte
}

func newHandlerFixture(t *testing.T, allowScripts bool) *handlerFixture {
	t.Helper()
	st, _ := newTestStore(t, DefaultLimits(), nil)
	ctx := context.Background()
	data := pngBytes(t, 16, 8)
	pngMeta, err := st.Put(ctx, bytes.NewReader(data), PutOptions{OriginalName: "картинка \"1\".png"})
	require.NoError(t, err)
	htmlMeta, err := st.Put(ctx, bytes.NewReader([]byte("<!DOCTYPE html><p>hi</p>")), PutOptions{OriginalName: "q.html"})
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.Handle("/files/", http.StripPrefix("/files", Handler(st, allowScripts)))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &handlerFixture{srv: srv, store: st, png: pngMeta, html: htmlMeta, data: data}
}

func (f *handlerFixture) do(t *testing.T, method, path string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, f.srv.URL+path, nil)
	require.NoError(t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := f.srv.Client().Do(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	resp.Body.Close()
	return resp, body
}

func TestHandlerFull(t *testing.T) {
	f := newHandlerFixture(t, false)
	resp, body := f.do(t, http.MethodGet, "/files/"+f.png.ID, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, f.data, body)
	require.Equal(t, "image/png", resp.Header.Get("Content-Type"))
	require.Equal(t, `"`+f.png.ID+`"`, resp.Header.Get("ETag"))
	require.Equal(t, "public, max-age=31536000, immutable", resp.Header.Get("Cache-Control"))
	require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
	require.Equal(t, "bytes", resp.Header.Get("Accept-Ranges"))
	require.Equal(t, strconv.Itoa(len(f.data)), resp.Header.Get("Content-Length"))
	require.NotEmpty(t, resp.Header.Get("Last-Modified"))
	require.Empty(t, resp.Header.Get("Content-Security-Policy"))
	require.Equal(t, `inline; filename="________ _1_.png"; filename*=UTF-8''%D0%BA%D0%B0%D1%80%D1%82%D0%B8%D0%BD%D0%BA%D0%B0%20%221%22.png`,
		resp.Header.Get("Content-Disposition"))
}

func TestHandlerRange(t *testing.T) {
	f := newHandlerFixture(t, false)
	resp, body := f.do(t, http.MethodGet, "/files/"+f.png.ID, map[string]string{"Range": "bytes=0-3"})
	require.Equal(t, http.StatusPartialContent, resp.StatusCode)
	require.Equal(t, f.data[:4], body)
	require.Equal(t, fmt.Sprintf("bytes 0-3/%d", len(f.data)), resp.Header.Get("Content-Range"))
	require.Equal(t, "4", resp.Header.Get("Content-Length"))
	require.Equal(t, "image/png", resp.Header.Get("Content-Type"))

	resp, body = f.do(t, http.MethodGet, "/files/"+f.png.ID, map[string]string{"Range": "bytes=-2"})
	require.Equal(t, http.StatusPartialContent, resp.StatusCode)
	require.Equal(t, f.data[len(f.data)-2:], body)

	resp, _ = f.do(t, http.MethodGet, "/files/"+f.png.ID, map[string]string{"Range": fmt.Sprintf("bytes=%d-", len(f.data)+100)})
	require.Equal(t, http.StatusRequestedRangeNotSatisfiable, resp.StatusCode)
	require.Equal(t, fmt.Sprintf("bytes */%d", len(f.data)), resp.Header.Get("Content-Range"))

	resp, _ = f.do(t, http.MethodGet, "/files/"+f.png.ID, map[string]string{"Range": "bytes=abc"})
	require.Equal(t, http.StatusRequestedRangeNotSatisfiable, resp.StatusCode)

	// If-Range with a stale ETag falls back to the full body.
	resp, body = f.do(t, http.MethodGet, "/files/"+f.png.ID, map[string]string{"Range": "bytes=0-3", "If-Range": `"stale"`})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, f.data, body)
}

func TestHandlerConditional(t *testing.T) {
	f := newHandlerFixture(t, false)
	resp, body := f.do(t, http.MethodGet, "/files/"+f.png.ID, map[string]string{"If-None-Match": `"` + f.png.ID + `"`})
	require.Equal(t, http.StatusNotModified, resp.StatusCode)
	require.Empty(t, body)
	require.Equal(t, `"`+f.png.ID+`"`, resp.Header.Get("ETag"))

	resp, _ = f.do(t, http.MethodGet, "/files/"+f.png.ID, map[string]string{"If-None-Match": `"other"`})
	require.Equal(t, http.StatusOK, resp.StatusCode)

	lm := resp.Header.Get("Last-Modified")
	resp, _ = f.do(t, http.MethodGet, "/files/"+f.png.ID, map[string]string{"If-Modified-Since": lm})
	require.Equal(t, http.StatusNotModified, resp.StatusCode)
}

func TestHandlerHead(t *testing.T) {
	f := newHandlerFixture(t, false)
	resp, body := f.do(t, http.MethodHead, "/files/"+f.png.ID, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Empty(t, body)
	require.Equal(t, strconv.Itoa(len(f.data)), resp.Header.Get("Content-Length"))
	require.Equal(t, "image/png", resp.Header.Get("Content-Type"))
	require.Equal(t, `"`+f.png.ID+`"`, resp.Header.Get("ETag"))
}

func TestHandlerNotFoundAndMethods(t *testing.T) {
	f := newHandlerFixture(t, false)
	for _, p := range []string{
		"/files/abc",
		"/files/" + strings.ToUpper(f.png.ID),
		"/files/" + strings.Repeat("0", 64),
		"/files/" + f.png.ID + "/extra",
		"/files/",
		"/files/../etc/passwd",
	} {
		resp, _ := f.do(t, http.MethodGet, p, nil)
		require.Equal(t, http.StatusNotFound, resp.StatusCode, p)
	}
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions} {
		resp, _ := f.do(t, m, "/files/"+f.png.ID, nil)
		require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode, m)
		require.Equal(t, "GET, HEAD", resp.Header.Get("Allow"))
	}
}

func TestHandlerHTMLSandbox(t *testing.T) {
	f := newHandlerFixture(t, false)
	resp, body := f.do(t, http.MethodGet, "/files/"+f.html.ID, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "<!DOCTYPE html><p>hi</p>", string(body))
	require.Equal(t, "text/html; charset=utf-8", resp.Header.Get("Content-Type"))
	require.Equal(t, "sandbox allow-forms allow-pointer-lock allow-popups", resp.Header.Get("Content-Security-Policy"))
	require.Equal(t, "no-referrer", resp.Header.Get("Referrer-Policy"))
	require.Equal(t, `inline; filename="q.html"`, resp.Header.Get("Content-Disposition"))

	fs := newHandlerFixture(t, true)
	resp, _ = fs.do(t, http.MethodGet, "/files/"+fs.html.ID, nil)
	require.Equal(t, "sandbox allow-forms allow-pointer-lock allow-popups allow-scripts", resp.Header.Get("Content-Security-Policy"))
}

func TestHandlerDownload(t *testing.T) {
	f := newHandlerFixture(t, false)
	resp, _ := f.do(t, http.MethodGet, "/files/"+f.html.ID+"?download=1", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, `attachment; filename="q.html"`, resp.Header.Get("Content-Disposition"))

	resp, _ = f.do(t, http.MethodGet, "/files/"+f.html.ID+"?download=0", nil)
	require.Equal(t, `inline; filename="q.html"`, resp.Header.Get("Content-Disposition"))
}

func TestHandlerFallbackFilename(t *testing.T) {
	st, _ := newTestStore(t, DefaultLimits(), nil)
	m, err := st.Put(context.Background(), bytes.NewReader(gifBytes(t, 2, 2)), PutOptions{})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	Handler(st, false).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+m.ID, nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, `inline; filename="`+m.ID+`.gif"`, rec.Header().Get("Content-Disposition"))
}

func TestHandlerSVGHeaders(t *testing.T) {
	limits := DefaultLimits()
	limits.AllowSVG = true
	st, _ := newTestStore(t, limits, nil)
	m, err := st.Put(context.Background(), bytes.NewReader([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)), PutOptions{OriginalName: "v.svg"})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	Handler(st, true).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+m.ID, nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/svg+xml", rec.Header().Get("Content-Type"))
	require.Equal(t, "sandbox; script-src 'none'", rec.Header().Get("Content-Security-Policy"))
}

func TestHandlerStoreError(t *testing.T) {
	st := &failingStore{}
	rec := httptest.NewRecorder()
	Handler(st, false).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+strings.Repeat("a", 64), nil))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

type failingStore struct{ Store }

func (failingStore) Open(context.Context, string) (io.ReadSeekCloser, Meta, error) {
	return nil, Meta{}, fmt.Errorf("disk on fire")
}

func TestMountChi(t *testing.T) {
	st, _ := newTestStore(t, DefaultLimits(), nil)
	data := jpegBytes(t, 4, 4)
	m, err := st.Put(context.Background(), bytes.NewReader(data), PutOptions{OriginalName: "photo.jpg"})
	require.NoError(t, err)

	r := chi.NewRouter()
	Mount(r, st, false)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/media/" + m.ID)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, data, body)
	require.Equal(t, "image/jpeg", resp.Header.Get("Content-Type"))

	resp, err = http.Head(srv.URL + "/media/" + m.ID)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	resp, err = http.Get(srv.URL + "/media/nope")
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp, err = http.Post(srv.URL+"/media/"+m.ID, "text/plain", nil)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestContentDisposition(t *testing.T) {
	require.Equal(t, `inline; filename="a.png"`, contentDisposition("inline", "a.png"))
	require.Equal(t, `attachment; filename="_.png"; filename*=UTF-8''%C3%A9.png`, contentDisposition("attachment", "é.png"))
	require.Equal(t, `inline; filename="a_b.png"; filename*=UTF-8''a%22b.png`, contentDisposition("inline", `a"b.png`))
	require.Equal(t, `inline; filename="a b;c.png"`, contentDisposition("inline", "a b;c.png"))
	require.Equal(t, "a-b_c.d~e%20f%2F%3B%2A", rfc5987Encode("a-b_c.d~e f/;*"))
}
