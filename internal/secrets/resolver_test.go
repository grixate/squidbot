package secrets

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveEnv(t *testing.T) {
	t.Setenv("SQUIDBOT_TEST_SECRET", "abc123")
	resolver := NewResolver()
	value, err := resolver.Resolve("env:SQUIDBOT_TEST_SECRET")
	if err != nil {
		t.Fatalf("resolve env: %v", err)
	}
	if value != "abc123" {
		t.Fatalf("unexpected env value: %q", value)
	}
}

func TestResolveFilePermissions(t *testing.T) {
	resolver := NewResolver()
	path := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(path, []byte("value\n"), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}
	value, err := resolver.Resolve("file:" + path)
	if err != nil {
		t.Fatalf("resolve 0600 file: %v", err)
	}
	if value != "value" {
		t.Fatalf("unexpected file value: %q", value)
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		if _, err := resolver.Resolve("file:" + path); err == nil {
			t.Fatal("expected permission error for world-readable secret file")
		}
	}
}

func TestResolveMap(t *testing.T) {
	t.Setenv("SQUIDBOT_MAP_SECRET", "token-1")
	resolver := NewResolver()
	resolved, err := resolver.ResolveMap(map[string]string{
		"Authorization": SecretRefValue("env:SQUIDBOT_MAP_SECRET"),
		"X-Static":      "static",
	})
	if err != nil {
		t.Fatalf("resolve map: %v", err)
	}
	if resolved["Authorization"] != "token-1" {
		t.Fatalf("unexpected resolved auth header: %q", resolved["Authorization"])
	}
	if resolved["X-Static"] != "static" {
		t.Fatalf("unexpected static header: %q", resolved["X-Static"])
	}
}
