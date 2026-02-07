package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEditFileMultipleOccurrences(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "a.txt")
	if err := os.WriteFile(path, []byte("x\nx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	policy, err := NewPathPolicy(workspace)
	if err != nil {
		t.Fatal(err)
	}
	tool := NewEditFileTool(policy)
	args, _ := json.Marshal(map[string]string{
		"path":     "a.txt",
		"old_text": "x",
		"new_text": "y",
	})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text == "" || result.Text[:8] != "Warning:" {
		t.Fatalf("expected warning result, got: %s", result.Text)
	}
}
