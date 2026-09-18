// Package config loads server configuration from environment variables
// (prefix SIGAME_ / OPENROUTER_) with an optional YAML file underneath.
// Precedence: defaults < YAML file < environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"gopkg.in/yaml.v3"
)

// Config is the complete server configuration.
type Config struct {
	// Network
	Addr        string   `env:"SIGAME_ADDR" yaml:"addr" envDefault:":8080"`
	PublicURL   string   `env:"SIGAME_PUBLIC_URL" yaml:"publicUrl"`                      // e.g. https://quiz.example.com; empty = derive from LAN addresses
	CORSOrigins []string `env:"SIGAME_CORS_ORIGINS" yaml:"corsOrigins" envSeparator:","` // empty = allow all (LAN default)
	MDNS        bool     `env:"SIGAME_MDNS" yaml:"mdns" envDefault:"true"`
	TrustProxy  bool     `env:"SIGAME_TRUST_PROXY" yaml:"trustProxy" envDefault:"false"` // honour X-Forwarded-* from a reverse proxy

	// Storage
	DataDir string `env:"SIGAME_DATA_DIR" yaml:"dataDir" envDefault:"./data"`

	// Logging
	LogLevel  string `env:"SIGAME_LOG_LEVEL" yaml:"logLevel" envDefault:"info"`   // debug|info|warn|error
	LogFormat string `env:"SIGAME_LOG_FORMAT" yaml:"logFormat" envDefault:"text"` // text|json

	// Limits (MiB)
	MaxImageMB int64 `env:"SIGAME_MAX_IMAGE_MB" yaml:"maxImageMB" envDefault:"20"`
	MaxAudioMB int64 `env:"SIGAME_MAX_AUDIO_MB" yaml:"maxAudioMB" envDefault:"100"`
	MaxVideoMB int64 `env:"SIGAME_MAX_VIDEO_MB" yaml:"maxVideoMB" envDefault:"512"`
	MaxHTMLMB  int64 `env:"SIGAME_MAX_HTML_MB" yaml:"maxHTMLMB" envDefault:"1"`
	MaxSIQMB   int64 `env:"SIGAME_MAX_SIQ_MB" yaml:"maxSIQMB" envDefault:"1024"`

	// Media
	FFProbePath      string        `env:"SIGAME_FFPROBE" yaml:"ffprobe"` // empty = look up in PATH
	AllowHTMLScripts bool          `env:"SIGAME_ALLOW_HTML_SCRIPTS" yaml:"allowHtmlScripts" envDefault:"true"`
	AllowSVG         bool          `env:"SIGAME_ALLOW_SVG" yaml:"allowSvg" envDefault:"false"`
	MediaGCGrace     time.Duration `env:"SIGAME_MEDIA_GC_GRACE" yaml:"mediaGcGrace" envDefault:"24h"`

	// Rooms
	RoomTTL           time.Duration `env:"SIGAME_ROOM_TTL" yaml:"roomTtl" envDefault:"6h"` // idle rooms are closed after this
	MaxRooms          int           `env:"SIGAME_MAX_ROOMS" yaml:"maxRooms" envDefault:"50"`
	MaxPlayersPerRoom int           `env:"SIGAME_MAX_PLAYERS" yaml:"maxPlayers" envDefault:"12"`
	WSMaxMessageBytes int64         `env:"SIGAME_WS_MAX_MESSAGE_BYTES" yaml:"wsMaxMessageBytes" envDefault:"65536"`
	WSMessagesPerSec  int           `env:"SIGAME_WS_MESSAGES_PER_SEC" yaml:"wsMessagesPerSec" envDefault:"40"`

	// AI showman (OpenRouter)
	OpenRouterAPIKey  string        `env:"OPENROUTER_API_KEY" yaml:"openRouterApiKey"`
	OpenRouterModel   string        `env:"OPENROUTER_MODEL" yaml:"openRouterModel" envDefault:"tencent/hy4-preview"`
	OpenRouterBaseURL string        `env:"OPENROUTER_BASE_URL" yaml:"openRouterBaseUrl" envDefault:"https://openrouter.ai/api/v1"`
	AIJudgeTimeout    time.Duration `env:"SIGAME_AI_TIMEOUT" yaml:"aiTimeout" envDefault:"8s"`
	AITemperature     float64       `env:"SIGAME_AI_TEMPERATURE" yaml:"aiTemperature" envDefault:"0"`
	AIMaxTokens       int           `env:"SIGAME_AI_MAX_TOKENS" yaml:"aiMaxTokens" envDefault:"200"`

	// Misc
	Dev   bool `env:"SIGAME_DEV" yaml:"dev" envDefault:"false"` // dev mode: pprof, verbose errors
	PProf bool `env:"SIGAME_PPROF" yaml:"pprof" envDefault:"false"`
}

// Load reads the optional YAML file (path from SIGAME_CONFIG, default
// "config.yaml" if it exists) and then overrides with environment variables.
func Load() (*Config, error) {
	cfg := &Config{}
	// Defaults first (env parsing applies envDefault even when the var is unset).
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("config: defaults: %w", err)
	}
	path := os.Getenv("SIGAME_CONFIG")
	if path == "" {
		if _, err := os.Stat("config.yaml"); err == nil {
			path = "config.yaml"
		}
	}
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("config: read %s: %w", path, err)
		}
		if err := yaml.Unmarshal(b, cfg); err != nil {
			return nil, fmt.Errorf("config: parse %s: %w", path, err)
		}
	}
	// Environment overrides the file. Defaults must not be re-applied here
	// (they would clobber YAML values), so point the default tag at a name no
	// field uses.
	if err := env.ParseWithOptions(cfg, env.Options{DefaultValueTagName: "envDefaultDisabled"}); err != nil {
		return nil, fmt.Errorf("config: env: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks value ranges and normalizes derived fields.
func (c *Config) Validate() error {
	var errs []error
	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
		c.LogLevel = strings.ToLower(c.LogLevel)
	default:
		errs = append(errs, fmt.Errorf("SIGAME_LOG_LEVEL must be debug|info|warn|error, got %q", c.LogLevel))
	}
	switch strings.ToLower(c.LogFormat) {
	case "text", "json":
		c.LogFormat = strings.ToLower(c.LogFormat)
	default:
		errs = append(errs, fmt.Errorf("SIGAME_LOG_FORMAT must be text|json, got %q", c.LogFormat))
	}
	if c.Addr == "" {
		errs = append(errs, errors.New("SIGAME_ADDR must not be empty"))
	}
	if c.DataDir == "" {
		errs = append(errs, errors.New("SIGAME_DATA_DIR must not be empty"))
	}
	for name, v := range map[string]int64{"SIGAME_MAX_IMAGE_MB": c.MaxImageMB, "SIGAME_MAX_AUDIO_MB": c.MaxAudioMB, "SIGAME_MAX_VIDEO_MB": c.MaxVideoMB, "SIGAME_MAX_HTML_MB": c.MaxHTMLMB, "SIGAME_MAX_SIQ_MB": c.MaxSIQMB} {
		if v <= 0 {
			errs = append(errs, fmt.Errorf("%s must be > 0", name))
		}
	}
	if c.MaxPlayersPerRoom < 1 || c.MaxPlayersPerRoom > 12 {
		errs = append(errs, errors.New("SIGAME_MAX_PLAYERS must be 1..12"))
	}
	if c.AIJudgeTimeout < 500*time.Millisecond {
		errs = append(errs, errors.New("SIGAME_AI_TIMEOUT must be ≥ 500ms"))
	}
	c.PublicURL = strings.TrimRight(c.PublicURL, "/")
	return errors.Join(errs...)
}

// AIConfigured reports whether the AI showman can be used.
func (c *Config) AIConfigured() bool { return c.OpenRouterAPIKey != "" }

// Redacted returns a copy safe for logging.
func (c Config) Redacted() Config {
	if c.OpenRouterAPIKey != "" {
		c.OpenRouterAPIKey = "***"
	}
	return c
}
