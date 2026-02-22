package contextctrl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRegistryResolveExactAndAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model-windows.json")
	writeFile(t, path, `{
  "version": 1,
  "defaults": {"contextWindowTokens": 4096, "outputReserveTokens": 512, "charsPerToken": 4.0},
  "models": [
    {
      "name": "gemma3:4b",
      "aliases": ["gemma-3-4b", "google/gemma-3-4b-it"],
      "contextWindowTokens": 8192,
      "outputReserveTokens": 1024,
      "charsPerToken": 3.6
    }
  ]
}`)

	reg := NewRegistry(path, 2048, 256, 5.0, nil)
	exact, err := reg.Resolve("gemma3:4b")
	if err != nil {
		t.Fatalf("resolve exact failed: %v", err)
	}
	if exact.ContextWindowTokens != 8192 || exact.OutputReserveTokens != 1024 {
		t.Fatalf("unexpected exact profile: %#v", exact)
	}
	if exact.Source != "registry:model" {
		t.Fatalf("unexpected source: %s", exact.Source)
	}

	alias, err := reg.Resolve("google/gemma-3-4b-it")
	if err != nil {
		t.Fatalf("resolve alias failed: %v", err)
	}
	if alias.ContextWindowTokens != 8192 || alias.OutputReserveTokens != 1024 {
		t.Fatalf("unexpected alias profile: %#v", alias)
	}
	if alias.Source != "registry:model_alias" {
		t.Fatalf("unexpected alias source: %s", alias.Source)
	}

	defaulted, err := reg.Resolve("unknown-model")
	if err != nil {
		t.Fatalf("resolve defaults failed: %v", err)
	}
	if defaulted.ContextWindowTokens != 4096 || defaulted.OutputReserveTokens != 512 {
		t.Fatalf("unexpected default profile: %#v", defaulted)
	}
	if defaulted.Source != "registry:defaults" {
		t.Fatalf("unexpected default source: %s", defaulted.Source)
	}
}

func TestRegistryKeepsLastGoodOnParseFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model-windows.json")
	logs := make([]string, 0, 2)
	reg := NewRegistry(path, 2048, 256, 4.0, func(format string, args ...any) {
		logs = append(logs, format)
	})

	writeFile(t, path, `{
  "version": 1,
  "defaults": {"contextWindowTokens": 8192, "outputReserveTokens": 1024, "charsPerToken": 3.9},
  "models": [{"name":"gemma3:4b","contextWindowTokens":8192,"outputReserveTokens":1024,"charsPerToken":3.6}]
}`)
	first, err := reg.Resolve("gemma3:4b")
	if err != nil {
		t.Fatalf("expected first resolve to succeed, got %v", err)
	}
	if first.ContextWindowTokens != 8192 {
		t.Fatalf("unexpected first profile: %#v", first)
	}

	time.Sleep(5 * time.Millisecond)
	writeFile(t, path, `{not-json`)
	second, err := reg.Resolve("gemma3:4b")
	if err == nil {
		t.Fatal("expected parse failure error")
	}
	if second.ContextWindowTokens != 8192 || second.OutputReserveTokens != 1024 {
		t.Fatalf("expected cached profile on parse failure, got %#v", second)
	}
	if len(logs) == 0 {
		t.Fatal("expected parse failure log entry")
	}
}

func TestRegistryReloadsOnMtimeChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model-windows.json")
	reg := NewRegistry(path, 2048, 256, 4.0, nil)

	writeFile(t, path, `{"version":1,"defaults":{"contextWindowTokens":4096,"outputReserveTokens":256,"charsPerToken":4.0}}`)
	first, err := reg.Resolve("any")
	if err != nil {
		t.Fatalf("first resolve failed: %v", err)
	}
	if first.ContextWindowTokens != 4096 {
		t.Fatalf("unexpected first defaults: %#v", first)
	}

	time.Sleep(5 * time.Millisecond)
	writeFile(t, path, `{"version":1,"defaults":{"contextWindowTokens":8192,"outputReserveTokens":512,"charsPerToken":3.8}}`)
	second, err := reg.Resolve("any")
	if err != nil {
		t.Fatalf("second resolve failed: %v", err)
	}
	if second.ContextWindowTokens != 8192 || second.OutputReserveTokens != 512 {
		t.Fatalf("expected updated defaults after mtime change, got %#v", second)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatal(err)
	}
}
