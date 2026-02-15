package skills

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	fd, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fd.Close()
	writer := zip.NewWriter(fd)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestZipCacheExtractIdempotent(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "skill.zip")
	writeZip(t, zipPath, map[string]string{"SKILL.md": "# Skill\nBody", "references/a.md": "ref"})
	cache := NewZipCache(filepath.Join(tmp, "cache"))
	first, err := cache.Extract(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := cache.Extract(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("expected same extraction path, got %s vs %s", first, second)
	}
	if _, err := os.Stat(filepath.Join(first, "SKILL.md")); err != nil {
		t.Fatalf("expected extracted SKILL.md: %v", err)
	}
}

func TestZipCacheRejectsPathTraversal(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "bad.zip")
	writeZip(t, zipPath, map[string]string{"../evil.txt": "x", "SKILL.md": "# Skill"})
	cache := NewZipCache(filepath.Join(tmp, "cache"))
	if _, err := cache.Extract(zipPath); err == nil {
		t.Fatal("expected traversal path to fail")
	}
}
