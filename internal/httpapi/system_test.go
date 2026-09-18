package httpapi

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSystemHealthAndInfo(t *testing.T) {
	e := newTestEnv(t, withDeps(func(d *Deps) {
		d.Cfg.PublicURL = "https://quiz.example.test"
		d.FFProbe = true
	}))

	rec := e.do(t, http.MethodGet, "/api/v1/system/health", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var h Health
	decodeJSON(t, rec, &h)
	require.Equal(t, "ok", h.Status)
	require.True(t, h.DBOk)
	require.GreaterOrEqual(t, h.UptimeSec, int64(0))

	rec = e.do(t, http.MethodGet, "/api/v1/system/info", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var info SystemInfo
	decodeJSON(t, rec, &info)
	require.Equal(t, "test", info.Version)
	require.Equal(t, runtime.Version(), info.GoVersion)
	require.Equal(t, runtime.GOOS, info.OS)
	require.NotEmpty(t, info.JoinURLs)
	require.Equal(t, "https://quiz.example.test", info.JoinURLs[0])
	require.NotNil(t, info.LanAddresses)
	require.True(t, info.FFProbe)
	require.False(t, info.AIConfigured)
	require.Equal(t, int64(64), info.Limits.MaxSIQMB)
	require.Equal(t, int64(20), info.Limits.MaxImageMB)
	require.Equal(t, 12, info.MaxPlayers)
	require.NotZero(t, info.StartedAt)

	// Root page and docs links.
	rec = e.do(t, http.MethodGet, "/", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	require.Contains(t, rec.Body.String(), "/docs")
	require.Contains(t, rec.Body.String(), "/openapi.json")
}

func TestSystemHealthDegraded(t *testing.T) {
	e := newTestEnv(t, withDeps(func(d *Deps) {
		d.Ping = func(context.Context) error { return errors.New("db down") }
	}))
	rec := e.do(t, http.MethodGet, "/api/v1/system/health", nil)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	var h Health
	decodeJSON(t, rec, &h)
	require.Equal(t, "degraded", h.Status)
	require.False(t, h.DBOk)
}

func TestCORS(t *testing.T) {
	e := newTestEnv(t)

	// Preflight with the permissive default.
	rec := e.do(t, http.MethodOptions, "/api/v1/packs", nil,
		"Origin", "http://localhost:5173", "Access-Control-Request-Method", "POST")
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Contains(t, rec.Header().Get("Access-Control-Allow-Headers"), "If-Match")
	require.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), "DELETE")
	require.Equal(t, "600", rec.Header().Get("Access-Control-Max-Age"))

	rec = e.do(t, http.MethodGet, "/api/v1/system/health", nil, "Origin", "http://localhost:5173")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Contains(t, rec.Header().Get("Access-Control-Expose-Headers"), "ETag")

	// Restricted origins: exact match only.
	e2 := newTestEnv(t, withDeps(func(d *Deps) { d.Cfg.CORSOrigins = []string{"https://app.example.test"} }))
	rec = e2.do(t, http.MethodGet, "/api/v1/system/health", nil, "Origin", "https://app.example.test")
	require.Equal(t, "https://app.example.test", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Contains(t, rec.Header().Values("Vary"), "Origin")
	rec = e2.do(t, http.MethodGet, "/api/v1/system/health", nil, "Origin", "https://evil.example.test")
	require.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	rec = e2.do(t, http.MethodOptions, "/api/v1/packs", nil,
		"Origin", "https://evil.example.test", "Access-Control-Request-Method", "POST")
	require.NotEqual(t, http.StatusNoContent, rec.Code)
}

func TestRecoverer(t *testing.T) {
	e := newTestEnv(t)
	e.srv.Router.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	rec := e.do(t, http.MethodGet, "/boom", nil)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, http.StatusInternalServerError, decodeProblem(t, rec).Status)
}

func TestNewValidatesDeps(t *testing.T) {
	_, err := New(Deps{})
	require.Error(t, err)
}
