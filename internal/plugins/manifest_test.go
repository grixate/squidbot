package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverManifests(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "alpha")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
  "name":"alpha",
  "version":"1.0.0",
  "command":"/bin/echo",
  "tools":[{"name":"ping","description":"Ping","schema":{"type":"object"}}]
}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	found, err := discoverManifests([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("expected 1 manifest, got %d", len(found))
	}
	if found[0].Name != "alpha" {
		t.Fatalf("unexpected manifest name: %s", found[0].Name)
	}
}

func TestValidateManifestRejectsUnsupportedCapability(t *testing.T) {
	err := validateManifest(Manifest{
		Name:         "bad",
		Version:      "1",
		Command:      "/bin/echo",
		Capabilities: []string{"root"},
		Tools: []ToolManifest{{
			Name:        "x",
			Description: "x",
			Schema:      map[string]any{"type": "object"},
		}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
