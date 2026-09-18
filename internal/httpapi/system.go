package httpapi

import (
	"context"
	"net/http"
	"reflect"
	"runtime"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"sigame/internal/lan"
)

// Health is the answer of GET /system/health.
type Health struct {
	Status    string `json:"status" enum:"ok,degraded" doc:"ok, or degraded when the database does not answer"`
	UptimeSec int64  `json:"uptimeSec" doc:"Seconds since the server started"`
	DBOk      bool   `json:"dbOk" doc:"The database answered a ping (true when no ping is configured)"`
}

// Limits are the effective upload limits in MiB.
type Limits struct {
	MaxImageMB int64 `json:"maxImageMB" doc:"Largest accepted image"`
	MaxAudioMB int64 `json:"maxAudioMB" doc:"Largest accepted audio file"`
	MaxVideoMB int64 `json:"maxVideoMB" doc:"Largest accepted video file"`
	MaxHTMLMB  int64 `json:"maxHTMLMB" doc:"Largest accepted html file"`
	MaxSIQMB   int64 `json:"maxSIQMB" doc:"Largest accepted .siq archive on import"`
}

// SystemInfo is the answer of GET /system/info.
type SystemInfo struct {
	Version      string        `json:"version" doc:"Server version"`
	GoVersion    string        `json:"goVersion" doc:"Go runtime version"`
	OS           string        `json:"os" doc:"Operating system (GOOS)"`
	Arch         string        `json:"arch" doc:"CPU architecture (GOARCH)"`
	StartedAt    int64         `json:"startedAt" doc:"Unix ms UTC"`
	LanAddresses []lan.Address `json:"lanAddresses" doc:"Non-loopback addresses of this machine, private IPv4 first"`
	JoinURLs     []string      `json:"joinUrls" doc:"Base URLs players can open; the public URL first when configured"`
	FFProbe      bool          `json:"ffprobe" doc:"ffprobe is available for media probing"`
	AIConfigured bool          `json:"aiConfigured" doc:"An AI judge is configured"`
	Limits       Limits        `json:"limits"`
	MaxPlayers   int           `json:"maxPlayers" doc:"Maximum players per room"`
}

type healthOutput struct {
	Status int
	Body   Health
}

type infoOutput struct {
	Body SystemInfo
}

func (s *Server) registerSystem() {
	healthRef := s.API.OpenAPI().Components.Schemas.Schema(reflect.TypeOf(Health{}), true, "Health")
	huma.Register(s.API, huma.Operation{
		OperationID: "getHealth",
		Method:      http.MethodGet,
		Path:        basePath + "/system/health",
		Tags:        []string{tagSystem},
		Summary:     "Health check",
		Description: "Answers 200 with status ok when the server and its database are up, 503 with status degraded (same body shape) when the database ping fails.",
		Responses: map[string]*huma.Response{
			"503": {
				Description: "Database unreachable; the body is the same Health object with status degraded",
				Content:     map[string]*huma.MediaType{"application/json": {Schema: healthRef}},
			},
		},
	}, s.getHealth)

	huma.Register(s.API, huma.Operation{
		OperationID: "getSystemInfo",
		Method:      http.MethodGet,
		Path:        basePath + "/system/info",
		Tags:        []string{tagSystem},
		Summary:     "Server information",
		Description: "Version, runtime, LAN addresses with ready-to-share join URLs, feature availability (ffprobe, AI) and the effective limits.",
	}, s.getSystemInfo)
}

func (s *Server) getHealth(ctx context.Context, _ *struct{}) (*healthOutput, error) {
	out := &healthOutput{Status: http.StatusOK, Body: Health{
		Status:    "ok",
		UptimeSec: int64(time.Since(s.deps.StartedAt).Seconds()),
		DBOk:      true,
	}}
	if s.deps.Ping != nil {
		if err := s.deps.Ping(ctx); err != nil {
			s.log.Warn("health: database ping failed", "err", err)
			out.Status = http.StatusServiceUnavailable
			out.Body.Status = "degraded"
			out.Body.DBOk = false
		}
	}
	return out, nil
}

func (s *Server) getSystemInfo(_ context.Context, _ *struct{}) (*infoOutput, error) {
	cfg := s.deps.Cfg
	addrs, err := lan.Addresses()
	if err != nil {
		s.log.Warn("system info: cannot list interfaces", "err", err)
	}
	if addrs == nil {
		addrs = []lan.Address{}
	}
	urls := lan.JoinURLs(cfg.Addr, cfg.PublicURL, addrs)
	if urls == nil {
		urls = []string{}
	}
	return &infoOutput{Body: SystemInfo{
		Version:      s.deps.Version,
		GoVersion:    runtime.Version(),
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		StartedAt:    s.deps.StartedAt.UnixMilli(),
		LanAddresses: addrs,
		JoinURLs:     urls,
		FFProbe:      s.deps.FFProbe,
		AIConfigured: s.aiConfigured(context.Background()),
		Limits: Limits{
			MaxImageMB: cfg.MaxImageMB,
			MaxAudioMB: cfg.MaxAudioMB,
			MaxVideoMB: cfg.MaxVideoMB,
			MaxHTMLMB:  cfg.MaxHTMLMB,
			MaxSIQMB:   cfg.MaxSIQMB,
		},
		MaxPlayers: cfg.MaxPlayersPerRoom,
	}}, nil
}
