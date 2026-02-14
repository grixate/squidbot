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
	if !cfg.Runtime.Subagents.Enabled {
		t.Fatal("expected subagents.enabled default true")
	}
	if cfg.Runtime.Subagents.MaxConcurrent != 4 {
		t.Fatalf("unexpected subagents.maxConcurrent default: %d", cfg.Runtime.Subagents.MaxConcurrent)
	}
	if cfg.Runtime.Subagents.MaxQueue != 64 {
		t.Fatalf("unexpected subagents.maxQueue default: %d", cfg.Runtime.Subagents.MaxQueue)
	}
	if cfg.Runtime.Subagents.DefaultTimeoutSec != 300 {
		t.Fatalf("unexpected subagents.defaultTimeoutSec default: %d", cfg.Runtime.Subagents.DefaultTimeoutSec)
	}
	if cfg.Runtime.Subagents.MaxAttempts != 2 {
		t.Fatalf("unexpected subagents.maxAttempts default: %d", cfg.Runtime.Subagents.MaxAttempts)
	}
	if cfg.Runtime.Subagents.RetryBackoffSec != 8 {
		t.Fatalf("unexpected subagents.retryBackoffSec default: %d", cfg.Runtime.Subagents.RetryBackoffSec)
	}
	if cfg.Runtime.Subagents.MaxDepth != 1 {
		t.Fatalf("unexpected subagents.maxDepth default: %d", cfg.Runtime.Subagents.MaxDepth)
	}
	if cfg.Runtime.Subagents.AllowWrites {
		t.Fatal("expected subagents.allowWrites default false")
	}
	if !cfg.Runtime.Subagents.NotifyOnComplete {
		t.Fatal("expected subagents.notifyOnComplete default true")
	}
	if cfg.Runtime.Subagents.ReinjectCompletion {
		t.Fatal("expected subagents.reinjectCompletion default false")
	}
	if !cfg.Runtime.TokenSafety.Enabled {
		t.Fatal("expected tokenSafety.enabled default true")
	}
	if cfg.Runtime.TokenSafety.Mode != "hybrid" {
		t.Fatalf("unexpected tokenSafety.mode default: %q", cfg.Runtime.TokenSafety.Mode)
	}
	if cfg.Runtime.TokenSafety.GlobalHardLimitTokens == 0 {
		t.Fatal("expected tokenSafety.globalHardLimitTokens default > 0")
	}
	if cfg.Runtime.TokenSafety.SessionHardLimitTokens == 0 {
		t.Fatal("expected tokenSafety.sessionHardLimitTokens default > 0")
	}
	if cfg.Runtime.TokenSafety.SubagentRunHardLimitTokens == 0 {
		t.Fatal("expected tokenSafety.subagentRunHardLimitTokens default > 0")
	}
	if !cfg.Runtime.TokenSafety.EstimateOnMissingUsage {
		t.Fatal("expected tokenSafety.estimateOnMissingUsage default true")
	}
	if cfg.Runtime.TokenSafety.EstimateCharsPerToken != 4 {
		t.Fatalf("unexpected tokenSafety.estimateCharsPerToken default: %d", cfg.Runtime.TokenSafety.EstimateCharsPerToken)
	}
	if len(cfg.Runtime.TokenSafety.TrustedWriters) == 0 || cfg.Runtime.TokenSafety.TrustedWriters[0] != "cli:user" {
		t.Fatalf("unexpected tokenSafety.trustedWriters default: %#v", cfg.Runtime.TokenSafety.TrustedWriters)
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

func TestLoadAppliesTokenSafetyEnvOverrides(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SQUIDBOT_TOKEN_SAFETY_ENABLED", "false")
	t.Setenv("SQUIDBOT_TOKEN_SAFETY_MODE", "hard")
	t.Setenv("SQUIDBOT_TOKEN_SAFETY_GLOBAL_HARD_LIMIT_TOKENS", "555")
	t.Setenv("SQUIDBOT_TOKEN_SAFETY_ESTIMATE_CHARS_PER_TOKEN", "6")
	t.Setenv("SQUIDBOT_TOKEN_SAFETY_TRUSTED_WRITERS", "cli:user,*:42")

	cfg, err := Load(filepath.Join(t.TempDir(), "missing-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Runtime.TokenSafety.Enabled {
		t.Fatal("expected token safety disabled from env")
	}
	if cfg.Runtime.TokenSafety.Mode != "hard" {
		t.Fatalf("unexpected mode from env: %q", cfg.Runtime.TokenSafety.Mode)
	}
	if cfg.Runtime.TokenSafety.GlobalHardLimitTokens != 555 {
		t.Fatalf("unexpected global hard limit from env: %d", cfg.Runtime.TokenSafety.GlobalHardLimitTokens)
	}
	if cfg.Runtime.TokenSafety.EstimateCharsPerToken != 6 {
		t.Fatalf("unexpected estimate chars from env: %d", cfg.Runtime.TokenSafety.EstimateCharsPerToken)
	}
	if len(cfg.Runtime.TokenSafety.TrustedWriters) != 2 {
		t.Fatalf("unexpected trusted writers from env: %#v", cfg.Runtime.TokenSafety.TrustedWriters)
	}
}
