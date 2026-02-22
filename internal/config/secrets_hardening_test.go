package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPersistedConfigRejectsPlaintextSecrets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.json")
	raw := `{"providers":{"openai":{"apiKey":"sk-plain"}}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := LoadPersistedConfig(path)
	if err == nil {
		t.Fatal("expected plaintext secret rejection")
	}
	if !strings.Contains(err.Error(), "plaintext secrets") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRuntimeConfigDynamicChannelAuthTokenOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SQUIDBOT_CHANNEL_SLACK_AUTH_TOKEN", "top-secret")
	cfg, err := LoadRuntimeConfig(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	slack := cfg.Channels.Registry["slack"]
	if slack.AuthTokenRef != "env:SQUIDBOT_CHANNEL_SLACK_AUTH_TOKEN" {
		t.Fatalf("unexpected authTokenRef: %q", slack.AuthTokenRef)
	}
	if slack.TokenRef != "" {
		t.Fatalf("tokenRef should not be set for auth token override, got %q", slack.TokenRef)
	}
}

func TestSaveUsesSecurePermissions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	if err := Save(path, Default()); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected 0600 config file permissions, got %04o", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got&0o077 != 0 {
		t.Fatalf("expected private config dir permissions, got %04o", got)
	}
}

func TestLoadPersistedAndRuntimeSecretOverrides(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SQUIDBOT_OPENAI_API_KEY", "sk-runtime")
	persisted, err := LoadPersistedConfig(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("load persisted: %v", err)
	}
	if persisted.Providers.OpenAI.APIKeyRef != "" {
		t.Fatalf("persisted config should not include runtime env secret ref, got %q", persisted.Providers.OpenAI.APIKeyRef)
	}

	runtimeCfg, err := LoadRuntimeConfig(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if runtimeCfg.Providers.OpenAI.APIKeyRef != "env:SQUIDBOT_OPENAI_API_KEY" {
		t.Fatalf("unexpected runtime apiKeyRef: %q", runtimeCfg.Providers.OpenAI.APIKeyRef)
	}
	if runtimeCfg.Providers.OpenAI.APIKey != "sk-runtime" {
		t.Fatalf("unexpected runtime API key resolution")
	}
}
