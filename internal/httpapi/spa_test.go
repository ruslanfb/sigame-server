package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestSPAHandler(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":        {Data: []byte("<!doctype html><title>SIGame</title>")},
		"assets/app-abc.js": {Data: []byte("console.log(1)")},
		"favicon.svg":       {Data: []byte("<svg/>")},
	}
	h := spaHandler(fsys)
	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		return rec
	}

	rec := get("/")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "SIGame")
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	rec = get("/room/ABCDE/player")
	require.Equal(t, http.StatusOK, rec.Code, "client routes fall back to index")
	require.Contains(t, rec.Body.String(), "SIGame")

	rec = get("/assets/app-abc.js")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "console.log(1)", rec.Body.String())
	require.Contains(t, rec.Header().Get("Cache-Control"), "immutable")

	rec = get("/favicon.svg")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))

	rec = get("/missing.png")
	require.Equal(t, http.StatusNotFound, rec.Code, "file-like paths that do not exist are 404")

	for _, p := range []string{"/api/v1/nope", "/media/abc", "/ws", "/docs", "/openapi.json", "/schemas/X.json"} {
		rec = get(p)
		require.Equal(t, http.StatusNotFound, rec.Code, p)
		require.NotContains(t, rec.Body.String(), "SIGame", p)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)

	require.True(t, hasIndex(fsys))
	require.False(t, hasIndex(fstest.MapFS{}))
	require.False(t, hasIndex(nil))
}

func TestServerServesSPAWhenProvided(t *testing.T) {
	fsys := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>SIGame UI</title>")}}
	e := newTestEnv(t, withDeps(func(d *Deps) { d.WebFS = fsys }))
	rec := e.do(t, http.MethodGet, "/", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "SIGame UI")
	rec = e.do(t, http.MethodGet, "/room/ABCDE/table", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "SIGame UI")
	// API and docs keep working next to the SPA.
	rec = e.do(t, http.MethodGet, "/api/v1/system/health", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	rec = e.do(t, http.MethodGet, "/docs", nil)
	require.Equal(t, http.StatusOK, rec.Code)
}
