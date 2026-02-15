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
	if !cfg.Skills.Enabled {
		t.Fatal("expected skills.enabled default true")
	}
	if cfg.Skills.MaxActive != 3 {
		t.Fatalf("unexpected skills.maxActive default: %d", cfg.Skills.MaxActive)
	}
	if cfg.Skills.MatchThreshold != 35 {
		t.Fatalf("unexpected skills.matchThreshold default: %d", cfg.Skills.MatchThreshold)
	}
	if cfg.Skills.RefreshIntervalSec != 30 {
		t.Fatalf("unexpected skills.refreshIntervalSec default: %d", cfg.Skills.RefreshIntervalSec)
	}
	if cfg.Skills.PromptMaxChars != 12000 {
		t.Fatalf("unexpected skills.promptMaxChars default: %d", cfg.Skills.PromptMaxChars)
	}
	if cfg.Skills.SkillMaxChars != 4000 {
		t.Fatalf("unexpected skills.skillMaxChars default: %d", cfg.Skills.SkillMaxChars)
	}
	if !cfg.Skills.AllowZip {
		t.Fatal("expected skills.allowZip default true")
	}
	if cfg.Skills.CacheDir == "" {
		t.Fatal("expected skills.cacheDir default")
	}
	if cfg.Skills.Policy.Channels == nil {
		t.Fatal("expected skills.policy.channels default map")
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
	if cfg.Runtime.Federation.Enabled {
		t.Fatal("expected federation.enabled default false")
	}
	if cfg.Runtime.Federation.ListenAddr == "" {
		t.Fatal("expected federation.listenAddr default")
	}
	if cfg.Runtime.Federation.RequestTimeoutSec <= 0 {
		t.Fatal("expected federation.requestTimeoutSec > 0")
	}
	if cfg.Runtime.Federation.MaxRetries < 0 {
		t.Fatal("expected federation.maxRetries >= 0")
	}
	if cfg.Runtime.Federation.RetryBackoffMs < 0 {
		t.Fatal("expected federation.retryBackoffMs >= 0")
	}
	if !cfg.Runtime.Federation.AutoFallback {
		t.Fatal("expected federation.autoFallback default true")
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

func TestLoadAppliesSkillsEnvOverrides(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SQUIDBOT_SKILLS_ENABLED", "false")
	t.Setenv("SQUIDBOT_SKILLS_MAX_ACTIVE", "5")
	t.Setenv("SQUIDBOT_SKILLS_MATCH_THRESHOLD", "41")
	t.Setenv("SQUIDBOT_SKILLS_REFRESH_INTERVAL_SEC", "42")
	t.Setenv("SQUIDBOT_SKILLS_PROMPT_MAX_CHARS", "6000")
	t.Setenv("SQUIDBOT_SKILLS_SKILL_MAX_CHARS", "900")
	t.Setenv("SQUIDBOT_SKILLS_ALLOW_ZIP", "false")
	t.Setenv("SQUIDBOT_SKILLS_CACHE_DIR", "/tmp/skills-cache")

	cfg, err := Load(filepath.Join(t.TempDir(), "missing-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Skills.Enabled {
		t.Fatal("expected skills disabled from env")
	}
	if cfg.Skills.MaxActive != 5 {
		t.Fatalf("unexpected skills maxActive from env: %d", cfg.Skills.MaxActive)
	}
	if cfg.Skills.MatchThreshold != 41 {
		t.Fatalf("unexpected skills matchThreshold from env: %d", cfg.Skills.MatchThreshold)
	}
	if cfg.Skills.RefreshIntervalSec != 42 {
		t.Fatalf("unexpected skills refreshIntervalSec from env: %d", cfg.Skills.RefreshIntervalSec)
	}
	if cfg.Skills.PromptMaxChars != 6000 {
		t.Fatalf("unexpected skills promptMaxChars from env: %d", cfg.Skills.PromptMaxChars)
	}
	if cfg.Skills.SkillMaxChars != 900 {
		t.Fatalf("unexpected skills skillMaxChars from env: %d", cfg.Skills.SkillMaxChars)
	}
	if cfg.Skills.AllowZip {
		t.Fatal("expected skills allowZip false from env")
	}
	if cfg.Skills.CacheDir != "/tmp/skills-cache" {
		t.Fatalf("unexpected skills cacheDir from env: %s", cfg.Skills.CacheDir)
	}
}
