package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultIncludesMemoryAndSkillsSettings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := Default()
	if !cfg.Memory.Enabled {
		t.Fatal("expected memory.enabled default true")
	}
	if cfg.Memory.TopK != 8 {
		t.Fatalf("unexpected memory.topK default: %d", cfg.Memory.TopK)
	}
	if cfg.Memory.RecencyDays != 30 {
		t.Fatalf("unexpected memory.recencyDays default: %d", cfg.Memory.RecencyDays)
	}
	if cfg.Memory.EmbeddingsProvider != "none" {
		t.Fatalf("unexpected memory.embeddingsProvider default: %q", cfg.Memory.EmbeddingsProvider)
	}
	if len(cfg.Skills.Paths) != 1 {
		t.Fatalf("unexpected skills paths defaults: %#v", cfg.Skills.Paths)
	}
}

func TestLoadBackfillsMissingMemoryAndSkillsFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "legacy-config.json")
	raw := `{"agents":{"defaults":{"workspace":"","model":"anthropic/claude-opus-4-1","maxTokens":8192,"temperature":0.7,"maxToolIterations":20,"turnTimeoutSec":120,"toolTimeoutSec":60}},"providers":{},"channels":{"telegram":{"enabled":false,"token":"","allowFrom":[]}},"tools":{"web":{"search":{"apiKey":"","maxResults":5}}},"gateway":{"host":"0.0.0.0","port":18789},"storage":{"backend":"bbolt","dbPath":""},"runtime":{"mailboxSize":64,"actorIdleTtl":"15m0s","heartbeatIntervalSec":1800}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Memory.TopK != 8 || cfg.Memory.RecencyDays != 30 {
		t.Fatalf("expected memory defaults to be backfilled, got %#v", cfg.Memory)
	}
	if len(cfg.Skills.Paths) == 0 {
		t.Fatal("expected skills paths defaults to be backfilled")
	}
}

func TestLoadIgnoresLegacyManagementObject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "legacy-management-config.json")
	raw := `{"agents":{"defaults":{"workspace":"","model":"anthropic/claude-opus-4-1","maxTokens":8192,"temperature":0.7,"maxToolIterations":20,"turnTimeoutSec":120,"toolTimeoutSec":60}},"providers":{},"channels":{"telegram":{"enabled":false,"token":"","allowFrom":[]}},"tools":{"web":{"search":{"apiKey":"","maxResults":5}}},"gateway":{"host":"0.0.0.0","port":18789},"management":{"host":"127.0.0.1","port":18790,"publicBaseUrl":"","serveInGateway":true},"storage":{"backend":"bbolt","dbPath":""},"runtime":{"mailboxSize":64,"actorIdleTtl":"15m0s","heartbeatIntervalSec":1800}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Gateway.Port != 18789 {
		t.Fatalf("unexpected gateway port after loading legacy config: %d", cfg.Gateway.Port)
	}
}
