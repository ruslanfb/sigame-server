package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("SIGAME_CONFIG", "")
	wd := t.TempDir()
	chdir(t, wd)
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, ":8080", cfg.Addr)
	require.Equal(t, "./data", cfg.DataDir)
	require.Equal(t, "tencent/hy4-preview", cfg.OpenRouterModel)
	require.Equal(t, 8*time.Second, cfg.AIJudgeTimeout)
	require.True(t, cfg.MDNS)
	require.False(t, cfg.AIConfigured())
}

func TestLoadYAMLThenEnvOverride(t *testing.T) {
	wd := t.TempDir()
	chdir(t, wd)
	path := filepath.Join(wd, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("addr: ':9090'\nlogLevel: debug\nopenRouterApiKey: from-file\nmaxRooms: 7\n"), 0o600))
	t.Setenv("SIGAME_CONFIG", "")
	t.Setenv("SIGAME_ADDR", ":7070")
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, ":7070", cfg.Addr, "env overrides yaml")
	require.Equal(t, "debug", cfg.LogLevel)
	require.Equal(t, "from-file", cfg.OpenRouterAPIKey)
	require.Equal(t, 7, cfg.MaxRooms)
	require.Equal(t, "***", cfg.Redacted().OpenRouterAPIKey)
}

func TestValidateErrors(t *testing.T) {
	wd := t.TempDir()
	chdir(t, wd)
	t.Setenv("SIGAME_CONFIG", "")
	t.Setenv("SIGAME_LOG_LEVEL", "loud")
	t.Setenv("SIGAME_MAX_PLAYERS", "40")
	_, err := Load()
	require.Error(t, err)
	require.Contains(t, err.Error(), "SIGAME_LOG_LEVEL")
	require.Contains(t, err.Error(), "SIGAME_MAX_PLAYERS")
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(old) })
}
