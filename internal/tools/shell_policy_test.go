package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecToolPolicyDisabled(t *testing.T) {
	policy, err := NewPathPolicy(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tool := NewExecToolWithPolicy(policy, 0, ExecPolicy{Enabled: false})
	args, _ := json.Marshal(map[string]any{"command": "echo hi"})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Text, "policy_denied") {
		t.Fatalf("expected policy denial, got %q", result.Text)
	}
}

func TestExecToolPolicyBlockedCommand(t *testing.T) {
	policy, err := NewPathPolicy(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tool := NewExecToolWithPolicy(policy, 0, ExecPolicy{Enabled: true, BlockedCommands: []string{"rm"}})
	args, _ := json.Marshal(map[string]any{"command": "echo hi && rm -rf /tmp/a"})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Text, "policy_denied") {
		t.Fatalf("expected policy denial, got %q", result.Text)
	}
}

func TestExecToolPolicyAllowlistRejectsControlOperators(t *testing.T) {
	workspace := t.TempDir()
	policy, err := NewPathPolicy(workspace)
	if err != nil {
		t.Fatal(err)
	}
	tool := NewExecToolWithPolicy(policy, 0, ExecPolicy{Enabled: true, AllowedCommands: []string{"echo"}})
	args, _ := json.Marshal(map[string]any{"command": "echo hi; pwd"})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Text, "policy_denied") {
		t.Fatalf("expected policy denial, got %q", result.Text)
	}
}

func TestExecToolPolicyAllowedCommandRuns(t *testing.T) {
	workspace := t.TempDir()
	policy, err := NewPathPolicy(workspace)
	if err != nil {
		t.Fatal(err)
	}
	tool := NewExecToolWithPolicy(policy, 0, ExecPolicy{Enabled: true, AllowedCommands: []string{"pwd"}})
	args, _ := json.Marshal(map[string]any{"command": "pwd"})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.TrimSpace(result.Text), filepath.Clean(workspace)) {
		t.Fatalf("expected pwd output to include workspace path, got %q", result.Text)
	}
}
