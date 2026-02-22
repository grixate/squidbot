package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type stubTool struct {
	name        string
	description string
	schema      map[string]any
	result      ToolResult
	err         error
}

func (t *stubTool) Name() string           { return t.name }
func (t *stubTool) Description() string    { return t.description }
func (t *stubTool) Schema() map[string]any { return t.schema }
func (t *stubTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.err != nil {
		return ToolResult{}, t.err
	}
	return t.result, nil
}

func TestRegistryExecuteAndDefinitions(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Execute(context.Background(), "missing", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected missing tool error")
	}

	tok := &stubTool{
		name:        "ok",
		description: "ok tool",
		schema:      map[string]any{"type": "object"},
		result:      ToolResult{Text: "done"},
	}
	errTool := &stubTool{name: "err", description: "err tool", schema: map[string]any{"type": "object"}, err: errors.New("boom")}
	r.Register(tok)
	r.Register(errTool)

	result, err := r.Execute(context.Background(), "ok", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "done" {
		t.Fatalf("unexpected tool result: %+v", result)
	}

	if _, err := r.Execute(context.Background(), "err", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected wrapped execute error")
	}

	names := map[string]struct{}{}
	for _, name := range r.Names() {
		names[name] = struct{}{}
	}
	if _, ok := names["ok"]; !ok {
		t.Fatal("expected ok tool in names")
	}
	if _, ok := names["err"]; !ok {
		t.Fatal("expected err tool in names")
	}

	defs := r.Definitions()
	if len(defs) != 2 {
		t.Fatalf("expected 2 definitions, got %d", len(defs))
	}
	seen := map[string]bool{}
	for _, def := range defs {
		seen[def.Name] = true
		if def.Description == "" {
			t.Fatalf("definition missing description: %+v", def)
		}
	}
	if !seen["ok"] || !seen["err"] {
		t.Fatalf("definitions missing expected tools: %+v", defs)
	}
}
