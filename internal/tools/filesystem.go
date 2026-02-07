package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ReadFileTool struct {
	policy *PathPolicy
}

func NewReadFileTool(policy *PathPolicy) *ReadFileTool {
	return &ReadFileTool{policy: policy}
}

func (t *ReadFileTool) Name() string { return "read_file" }
func (t *ReadFileTool) Description() string {
	return "Read the contents of a file at the given path."
}
func (t *ReadFileTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string", "description": "The file path to read"}}, "required": []string{"path"}}
}
func (t *ReadFileTool) Execute(_ context.Context, args json.RawMessage) (ToolResult, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	resolved, err := t.policy.Resolve(in.Path)
	if err != nil {
		return ToolResult{}, err
	}
	bytes, err := os.ReadFile(resolved)
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Text: string(bytes)}, nil
}

type WriteFileTool struct {
	policy *PathPolicy
}

func NewWriteFileTool(policy *PathPolicy) *WriteFileTool {
	return &WriteFileTool{policy: policy}
}

func (t *WriteFileTool) Name() string { return "write_file" }
func (t *WriteFileTool) Description() string {
	return "Write content to a file at the given path. Creates parent directories if needed."
}
func (t *WriteFileTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}, "required": []string{"path", "content"}}
}
func (t *WriteFileTool) Execute(_ context.Context, args json.RawMessage) (ToolResult, error) {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	resolved, err := t.policy.Resolve(in.Path)
	if err != nil {
		return ToolResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return ToolResult{}, err
	}
	if err := os.WriteFile(resolved, []byte(in.Content), 0o644); err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Text: fmt.Sprintf("Successfully wrote %d bytes to %s", len(in.Content), in.Path)}, nil
}

type EditFileTool struct {
	policy *PathPolicy
}

func NewEditFileTool(policy *PathPolicy) *EditFileTool {
	return &EditFileTool{policy: policy}
}

func (t *EditFileTool) Name() string { return "edit_file" }
func (t *EditFileTool) Description() string {
	return "Edit a file by replacing old_text with new_text. The old_text must exist exactly in the file."
}
func (t *EditFileTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "old_text": map[string]any{"type": "string"}, "new_text": map[string]any{"type": "string"}}, "required": []string{"path", "old_text", "new_text"}}
}
func (t *EditFileTool) Execute(_ context.Context, args json.RawMessage) (ToolResult, error) {
	var in struct {
		Path    string `json:"path"`
		OldText string `json:"old_text"`
		NewText string `json:"new_text"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	resolved, err := t.policy.Resolve(in.Path)
	if err != nil {
		return ToolResult{}, err
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		return ToolResult{}, err
	}
	text := string(content)
	count := strings.Count(text, in.OldText)
	if count == 0 {
		return ToolResult{Text: "Error: old_text not found in file. Make sure it matches exactly."}, nil
	}
	if count > 1 {
		return ToolResult{Text: fmt.Sprintf("Warning: old_text appears %d times. Please provide more context to make it unique.", count)}, nil
	}
	updated := strings.Replace(text, in.OldText, in.NewText, 1)
	if err := os.WriteFile(resolved, []byte(updated), 0o644); err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Text: fmt.Sprintf("Successfully edited %s", in.Path)}, nil
}

type ListDirTool struct {
	policy *PathPolicy
}

func NewListDirTool(policy *PathPolicy) *ListDirTool {
	return &ListDirTool{policy: policy}
}

func (t *ListDirTool) Name() string        { return "list_dir" }
func (t *ListDirTool) Description() string { return "List the contents of a directory." }
func (t *ListDirTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}}
}
func (t *ListDirTool) Execute(_ context.Context, args json.RawMessage) (ToolResult, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	resolved, err := t.policy.Resolve(in.Path)
	if err != nil {
		return ToolResult{}, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return ToolResult{}, err
	}
	if len(entries) == 0 {
		return ToolResult{Text: fmt.Sprintf("Directory %s is empty", in.Path)}, nil
	}
	items := make([]string, 0, len(entries))
	for _, e := range entries {
		prefix := "📄 "
		if e.IsDir() {
			prefix = "📁 "
		}
		items = append(items, prefix+e.Name())
	}
	sort.Strings(items)
	return ToolResult{Text: strings.Join(items, "\n")}, nil
}
