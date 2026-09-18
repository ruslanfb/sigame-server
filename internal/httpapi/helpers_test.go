package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"sigame/internal/config"
	"sigame/internal/db"
	"sigame/internal/media"
	"sigame/internal/packs"
)

type testEnv struct {
	srv   *Server
	repo  *packs.Repo
	store *media.DiskStore
	cfg   *config.Config
}

type envConfig struct {
	limits media.Limits
	deps   func(*Deps)
}

type envOpt func(*envConfig)

func withLimits(l media.Limits) envOpt { return func(c *envConfig) { c.limits = l } }

func withDeps(f func(*Deps)) envOpt { return func(c *envConfig) { c.deps = f } }

// newTestEnv builds a Server on an in-memory SQLite database with a disk
// media store in a temp dir and no probing.
func newTestEnv(t *testing.T, opts ...envOpt) *testEnv {
	t.Helper()
	ec := envConfig{limits: media.DefaultLimits()}
	for _, o := range opts {
		o(&ec)
	}
	ctx := context.Background()
	sqlDB, err := db.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(ctx, sqlDB))

	dir := t.TempDir()
	logger := slog.New(slog.DiscardHandler)
	store, err := media.NewDiskStore(sqlDB, filepath.Join(dir, "media"), ec.limits, media.NopProber{}, logger)
	require.NoError(t, err)

	cfg := &config.Config{
		Addr:              ":8080",
		DataDir:           dir,
		LogLevel:          "info",
		LogFormat:         "text",
		MaxImageMB:        20,
		MaxAudioMB:        100,
		MaxVideoMB:        512,
		MaxHTMLMB:         1,
		MaxSIQMB:          64,
		AllowHTMLScripts:  true,
		MaxPlayersPerRoom: 12,
		OpenRouterModel:   "tencent/hy4-preview",
		OpenRouterBaseURL: "https://openrouter.ai/api/v1",
	}
	repo := packs.NewRepo(sqlDB)
	deps := Deps{
		Cfg:     cfg,
		Packs:   repo,
		Media:   store,
		Ping:    sqlDB.PingContext,
		Version: "test",
		Logger:  logger,
	}
	if ec.deps != nil {
		ec.deps(&deps)
	}
	srv, err := New(deps)
	require.NoError(t, err)
	return &testEnv{srv: srv, repo: repo, store: store, cfg: cfg}
}

// do performs a request against the router. body may be nil, []byte,
// *bytes.Buffer (raw) or any value (JSON-encoded). headers are name/value pairs.
func (e *testEnv) do(t *testing.T, method, path string, body any, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	contentType := ""
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
	case *bytes.Buffer:
		rd = b
	case string:
		rd = bytes.NewReader([]byte(b))
		contentType = "application/json"
	default:
		raw, err := json.Marshal(b)
		require.NoError(t, err)
		rd = bytes.NewReader(raw)
		contentType = "application/json"
	}
	req := httptest.NewRequest(method, path, rd)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	require.Equal(t, 0, len(headers)%2, "headers must be name/value pairs")
	for i := 0; i < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return rec
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), v), "body: %s", rec.Body.String())
}

// problem is the decoded RFC 9457 document.
type problem struct {
	Status int    `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Errors []struct {
		Message  string `json:"message"`
		Location string `json:"location"`
		Value    any    `json:"value"`
	} `json:"errors"`
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) problem {
	t.Helper()
	require.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json", "body: %s", rec.Body.String())
	var p problem
	decodeJSON(t, rec, &p)
	return p
}

// multipartBody builds a multipart/form-data body with one file part.
func multipartBody(t *testing.T, field, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, filename)
	require.NoError(t, err)
	_, err = fw.Write(data)
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	return &buf, mw.FormDataContentType()
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 7), uint8(y * 11), uint8(x ^ y), 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "siq", name))
	require.NoError(t, err)
	return data
}

// samplePack returns a minimal valid pack with one round/theme/question.
func samplePack(name string) packs.Pack {
	return packs.Pack{
		Name: name,
		Tags: []string{"test"},
		Rounds: []packs.Round{{
			Name: "Round 1",
			Themes: []packs.Theme{{
				Name: "Theme 1",
				Questions: []packs.Question{{
					Price:  100,
					Params: packs.QuestionParams{Question: []packs.ContentItem{{Type: packs.ContentText, Text: "What?"}}},
					Right:  []string{"That"},
				}},
			}},
		}},
	}
}

// createPack posts p and returns the stored pack.
func (e *testEnv) createPack(t *testing.T, p packs.Pack) packs.Pack {
	t.Helper()
	rec := e.do(t, http.MethodPost, "/api/v1/packs", p)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var out packs.Pack
	decodeJSON(t, rec, &out)
	return out
}

// uploadPNG uploads a generated image and returns its metadata.
func (e *testEnv) uploadPNG(t *testing.T, w, h int) media.Meta {
	t.Helper()
	body, ct := multipartBody(t, "file", "pic.png", pngBytes(t, w, h))
	rec := e.do(t, http.MethodPost, "/api/v1/media", body, "Content-Type", ct)
	require.Contains(t, []int{http.StatusCreated, http.StatusOK}, rec.Code, rec.Body.String())
	var m media.Meta
	decodeJSON(t, rec, &m)
	return m
}
