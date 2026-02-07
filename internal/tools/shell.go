package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type ExecTool struct {
	policy  *PathPolicy
	timeout time.Duration
}

func NewExecTool(policy *PathPolicy, timeout time.Duration) *ExecTool {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &ExecTool{policy: policy, timeout: timeout}
}

func (t *ExecTool) Name() string { return "exec" }
func (t *ExecTool) Description() string {
	return "Execute a shell command and return its output. Use with caution."
}
func (t *ExecTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"command":     map[string]any{"type": "string", "description": "The shell command to execute"},
		"working_dir": map[string]any{"type": "string", "description": "Optional working directory"},
	}, "required": []string{"command"}}
}
func (t *ExecTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	var in struct {
		Command    string `json:"command"`
		WorkingDir string `json:"working_dir"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(in.Command) == "" {
		return ToolResult{}, fmt.Errorf("command required")
	}

	cwd, err := t.policy.Resolve(".")
	if err != nil {
		return ToolResult{}, err
	}
	if strings.TrimSpace(in.WorkingDir) != "" {
		cwd, err = t.policy.Resolve(in.WorkingDir)
		if err != nil {
			return ToolResult{}, err
		}
	}

	execCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "sh", "-lc", in.Command)
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()

	parts := []string{}
	if stdout.Len() > 0 {
		parts = append(parts, stdout.String())
	}
	if stderr.Len() > 0 {
		parts = append(parts, "STDERR:\n"+stderr.String())
	}
	if execCtx.Err() == context.DeadlineExceeded {
		parts = append(parts, fmt.Sprintf("Error: Command timed out after %s", t.timeout))
	}
	if err != nil && execCtx.Err() == nil {
		parts = append(parts, fmt.Sprintf("\nExit error: %v", err))
	}

	result := "(no output)"
	if len(parts) > 0 {
		result = strings.Join(parts, "\n")
	}
	const maxLen = 10000
	if len(result) > maxLen {
		result = result[:maxLen] + fmt.Sprintf("\n... (truncated, %d more chars)", len(result)-maxLen)
	}
	return ToolResult{Text: result}, nil
}
