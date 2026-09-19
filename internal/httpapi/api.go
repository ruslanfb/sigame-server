// Package httpapi exposes the SIGame server over HTTP: a huma-powered REST
// API under /api/v1 (packs, media, AI judge, system), interactive docs at
// /docs, the OpenAPI 3.1 document at /openapi.json and /openapi.yaml, a tiny
// landing page at / and media streaming with Range support at /media/{id}.
//
// Room endpoints (rooms.go) and buzzer presets (presets.go) are registered on
// the same huma API; every handler file exposes a register* method that New
// calls in order, so adding a group is a one-line change.
//
// Errors are RFC 9457 problem details (huma.ErrorModel). Domain errors of the
// integrated packages are translated by mapError (errors.go).
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"sigame/internal/ai"
	"sigame/internal/config"
	"sigame/internal/media"
	"sigame/internal/packs"
	"sigame/internal/room"
)

const (
	// basePath prefixes every JSON operation.
	basePath = "/api/v1"
	// jsonTimeout bounds JSON operations; uploads, exports and media
	// streaming are registered outside the timeout group.
	jsonTimeout = 60 * time.Second
	// maxJSONBodyBytes caps JSON request bodies. A pack can hold
	// 50 rounds x 30 themes x 30 questions of text, so it is generous.
	maxJSONBodyBytes = 32 << 20
	// bodyReadTimeout replaces huma's 5 s default for large pack bodies.
	bodyReadTimeout = 60 * time.Second
	// multipartOverhead is added to the size caps of multipart uploads to
	// account for boundaries and part headers.
	multipartOverhead = 1 << 20
)

// OpenAPI tags.
const (
	tagPacks  = "packs"
	tagMedia  = "media"
	tagRooms  = "rooms"
	tagBuzzer = "buzzer"
	tagAI     = "ai"
	tagSystem = "system"
)

// apiDescription is the OpenAPI info.description.
const apiDescription = "SIGame Server is a self-hosted backend for «Своя игра» (SIGame-compatible): " +
	"it stores question packs in its own JSON model with .siq import/export, stores and streams media, " +
	"and hosts LAN/Internet games over WebSocket. Interactive documentation lives at /docs; the " +
	"machine-readable specification is served at /openapi.json and /openapi.yaml, and stored media is " +
	"streamed with HTTP Range support at GET /media/{id}. Every error is an RFC 9457 problem document " +
	"(application/problem+json) with `status`, `title` and `detail`; validation failures add an `errors` " +
	"list whose `location` is a JSON pointer into the request body (e.g. /rounds/0/themes/2/name) and " +
	"whose `value` is the machine-readable problem code.\n\n" +
	"Rooms: a game is a room identified by a 5-character code. POST /rooms creates one from a stored pack " +
	"(rules, timers, buzzer preset or explicit buzzer settings, showman mode) and returns the room together " +
	"with a secret `hostToken`; it is shown once, so the creator must keep it. Anyone joins with " +
	"POST /rooms/{code}/join (name, role, optional password) and receives a `sessionToken`; the creator " +
	"sends the host token in the `X-Host-Token` header of that request to claim host rights (isHost). The " +
	"client then opens the WebSocket GET /ws?room=CODE&token=SESSION and plays over it (see docs/protocol.md); " +
	"leaving is POST /rooms/{code}/leave. The host-only REST operations (PATCH settings, kick/unban, " +
	"transfer-host, buzz-log, DELETE) require the `X-Host-Token` header: 401 when it is missing, 403 when it " +
	"does not match the room. GET /buzzer-presets lists the named buzzer configurations a room can start from."

// AIStatusProvider exposes the operator-facing state of the AI judge:
// status without network access, the upstream model list and a live test
// call. *ai.OpenRouterJudge satisfies it.
type AIStatusProvider interface {
	Status(ctx context.Context) ai.StatusInfo
	ListModels(ctx context.Context) ([]ai.ModelInfo, error)
	Test(ctx context.Context, sample ai.Request) (ai.Verdict, error)
}

var _ AIStatusProvider = (*ai.OpenRouterJudge)(nil)

// Deps are the collaborators of the HTTP API; cmd/sigame wires them.
type Deps struct {
	Cfg   *config.Config // required
	Packs *packs.Repo    // required
	Media media.Store    // required

	// MediaHandler serves GET/HEAD /media/{id}. Optional; the default is
	// media.Handler(Media, Cfg.AllowHTMLScripts).
	MediaHandler http.Handler

	// Rooms hosts the game rooms behind /rooms and is consulted by
	// /buzzer-presets. Optional: when nil every room operation answers 503.
	Rooms *room.Manager

	// AI is the judge used by POST /ai/test when AIStatus is nil. May be nil.
	AI ai.Judge
	// AIStatus reports the OpenRouter state (status/models/test). May be nil,
	// in which case the AI endpoints report "not configured".
	AIStatus AIStatusProvider

	// Ping checks the database for GET /system/health. Optional.
	Ping func(ctx context.Context) error
	// FFProbe reports whether ffprobe was found (shown in /system/info).
	FFProbe bool

	// WebFS is the built web client (web/dist). When it contains index.html
	// it is served at / with SPA fallback; otherwise / shows a docs landing page.
	WebFS fs.FS

	Version   string       // reported in OpenAPI info.version and /system/info; default "dev"
	Logger    *slog.Logger // default slog.Default()
	StartedAt time.Time    // default time.Now()
}

// Server is the assembled HTTP handler. Router carries every route (JSON
// API, docs, media, landing page); API is the huma instance other files
// (rooms.go) register additional operations on.
type Server struct {
	Router chi.Router
	API    huma.API

	deps         Deps
	log          *slog.Logger
	tmpDir       string // <DataDir>/tmp for spooled .siq uploads
	siqBodyCap   int64  // request body cap of POST /packs/import
	mediaBodyCap int64  // request body cap of POST /media
}

// New builds the router, the huma API and registers every operation.
func New(d Deps) (*Server, error) {
	switch {
	case d.Cfg == nil:
		return nil, errors.New("httpapi: Cfg is required")
	case d.Packs == nil:
		return nil, errors.New("httpapi: Packs is required")
	case d.Media == nil:
		return nil, errors.New("httpapi: Media is required")
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Version == "" {
		d.Version = "dev"
	}
	if d.StartedAt.IsZero() {
		d.StartedAt = time.Now()
	}
	if d.MediaHandler == nil {
		d.MediaHandler = media.Handler(d.Media, d.Cfg.AllowHTMLScripts)
	}

	tmpDir := filepath.Join(d.Cfg.DataDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return nil, fmt.Errorf("httpapi: create temp dir %s: %w", tmpDir, err)
	}

	s := &Server{
		deps:         d,
		log:          d.Logger,
		tmpDir:       tmpDir,
		siqBodyCap:   d.Cfg.MaxSIQMB<<20 + multipartOverhead,
		mediaBodyCap: max(d.Cfg.MaxImageMB, d.Cfg.MaxAudioMB, d.Cfg.MaxVideoMB, d.Cfg.MaxHTMLMB)<<20 + multipartOverhead,
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	if d.Cfg.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(requestLogger(s.log))
	r.Use(recoverer(s.log))
	r.Use(corsMiddleware(d.Cfg.CORSOrigins))

	// JSON operations (and huma's own /docs, /openapi.*, /schemas/*) run
	// under the request timeout; streaming routes are added to the root.
	jsonRouter := r.With(middleware.Timeout(jsonTimeout))
	api := humachi.New(jsonRouter, s.humaConfig())
	s.Router = r
	s.API = api

	if hasIndex(d.WebFS) {
		r.Handle("/*", spaHandler(d.WebFS))
	} else {
		r.Get("/", s.rootPage)
	}

	s.registerPacks()
	s.registerNested()
	s.registerMedia()
	s.registerRooms()
	s.registerPresets()
	s.registerAI()
	s.registerSystem()

	// Streaming routes: plain chi handlers, documented manually in openapi.go.
	r.Post(basePath+"/packs/import", s.importSiq)
	r.Get(basePath+"/packs/{id}/export", s.exportSiq)
	r.Post(basePath+"/media", s.uploadMedia)
	r.Method(http.MethodGet, "/media/{id}", d.MediaHandler)
	r.Method(http.MethodHead, "/media/{id}", d.MediaHandler)
	s.documentRawOperations()

	relaxServerFields(api.OpenAPI())
	return s, nil
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.Router.ServeHTTP(w, r) }

func (s *Server) humaConfig() huma.Config {
	cfg := huma.DefaultConfig("SIGame Server API", s.deps.Version)
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"hostToken": {Type: "apiKey", In: "header", Name: "X-Host-Token", Description: "Room creator secret returned by POST /rooms; required by the host-only room operations (401 when missing, 403 when wrong)."},
	}
	cfg.OpenAPIPath = "/openapi"
	cfg.DocsPath = "/docs"
	cfg.DocsRenderer = huma.DocsRendererScalar
	cfg.Info.Description = apiDescription
	cfg.Servers = nil
	cfg.Tags = []*huma.Tag{
		{Name: tagPacks, Description: "Question packs: CRUD, nested round/theme/question editing, validation, .siq import and export."},
		{Name: tagMedia, Description: "Content-addressed media store (images, audio, video, html): upload, metadata, deletion; streaming at GET /media/{id}."},
		{Name: tagRooms, Description: "Game rooms: create (host token), list, get, join (session token for /ws), leave, and the host-only settings, kick/ban, host transfer, buzz log and close."},
		{Name: tagBuzzer, Description: "Named buzzer presets (lanWired, wifiParty, internetFair, tournament, noRace) with their full settings."},
		{Name: tagAI, Description: "AI showman (OpenRouter) diagnostics: status, model list and a live judging test."},
		{Name: tagSystem, Description: "Health, version, LAN addresses and effective limits."},
	}
	return cfg
}

// rootPage is the only HTML the server produces besides /docs.
func (s *Server) rootPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, rootHTML, s.deps.Version)
}

const rootHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>SIGame Server</title>
<style>
body{font:16px/1.5 system-ui,sans-serif;max-width:40rem;margin:3rem auto;padding:0 1rem;color:#222}
code{background:#f3f3f3;padding:.1em .3em;border-radius:3px}
</style>
</head>
<body>
<h1>SIGame Server</h1>
<p>Backend for «Своя игра» (SIGame-compatible). Version <code>%s</code>.</p>
<ul>
<li><a href="/docs">Interactive API documentation</a></li>
<li><a href="/openapi.json">OpenAPI 3.1 (JSON)</a> · <a href="/openapi.yaml">OpenAPI 3.1 (YAML)</a></li>
<li><a href="/api/v1/system/info">System info</a> · <a href="/api/v1/system/health">Health</a></li>
</ul>
<p>Stored media is streamed at <code>GET /media/{id}</code> with HTTP Range support.</p>
</body>
</html>
`
