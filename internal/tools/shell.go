package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

var (
	errExecDisabled = errors.New("policy_denied: exec tool is disabled")
)

var shellControlPattern = regexp.MustCompile(`[;&|><` + "`" + `]|\$\(`)

type ExecPolicy struct {
	Enabled         bool
	AllowedCommands []string
	BlockedCommands []string
}

type ExecTool struct {
	policy  *PathPolicy
	timeout time.Duration
	config  ExecPolicy
}

func NewExecTool(policy *PathPolicy, timeout time.Duration) *ExecTool {
	return NewExecToolWithPolicy(policy, timeout, ExecPolicy{Enabled: true})
}

func NewExecToolWithPolicy(policy *PathPolicy, timeout time.Duration, cfg ExecPolicy) *ExecTool {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	cfg.AllowedCommands = normalizeCommandList(cfg.AllowedCommands)
	cfg.BlockedCommands = normalizeCommandList(cfg.BlockedCommands)
	return &ExecTool{policy: policy, timeout: timeout, config: cfg}
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
	if !t.config.Enabled {
		return ToolResult{Text: errExecDisabled.Error(), Metadata: map[string]any{"policy_denied": true, "reason": "disabled"}}, nil
	}
	decision, policyErr := evaluateExecPolicy(in.Command, t.config)
	if policyErr != nil {
		return ToolResult{Text: policyErr.Error(), Metadata: map[string]any{"policy_denied": true, "reason": decision}}, nil
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
	return ToolResult{Text: result, Metadata: map[string]any{"policy_denied": false, "policy_reason": decision}}, nil
}

func normalizeCommandList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, strings.ToLower(value))
	}
	return out
}

func evaluateExecPolicy(command string, cfg ExecPolicy) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "empty", fmt.Errorf("policy_denied: command is empty")
	}
	tokens := extractCommandTokens(command)
	if len(tokens) == 0 {
		return "unparseable", fmt.Errorf("policy_denied: command could not be parsed safely")
	}

	for _, token := range tokens {
		base := strings.ToLower(filepath.Base(token))
		if slices.Contains(cfg.BlockedCommands, base) || slices.Contains(cfg.BlockedCommands, strings.ToLower(token)) {
			return "blocked_command", fmt.Errorf("policy_denied: blocked command %q", base)
		}
	}

	if len(cfg.AllowedCommands) > 0 {
		root := strings.ToLower(filepath.Base(tokens[0]))
		if !slices.Contains(cfg.AllowedCommands, root) && !slices.Contains(cfg.AllowedCommands, strings.ToLower(tokens[0])) {
			return "not_allowlisted", fmt.Errorf("policy_denied: command %q is not allowlisted", root)
		}
		if shellControlPattern.MatchString(command) {
			return "shell_control_operator", fmt.Errorf("policy_denied: control operators are not allowed with allowlisted exec")
		}
	}
	return "allowed", nil
}

func extractCommandTokens(command string) []string {
	// Include path-like tokens to catch blocked commands hidden behind shell wrappers.
	re := regexp.MustCompile(`[A-Za-z0-9_./:-]+`)
	raw := re.FindAllString(command, -1)
	out := make([]string, 0, len(raw))
	for _, token := range raw {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		out = append(out, token)
	}
	return out
}
