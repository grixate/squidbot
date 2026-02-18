package provider

import (
	"strings"
	"testing"
)

func TestCodexConsumeSSEParsesContentAndToolCalls(t *testing.T) {
	payload := strings.Join([]string{
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hel\"}",
		"",
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"lo\"}",
		"",
		"data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"function_call\",\"call_id\":\"call_1\",\"id\":\"fc_1\",\"name\":\"echo\"}}",
		"",
		"data: {\"type\":\"response.function_call_arguments.delta\",\"call_id\":\"call_1\",\"delta\":\"{\\\"a\\\":\"}",
		"",
		"data: {\"type\":\"response.function_call_arguments.delta\",\"call_id\":\"call_1\",\"delta\":\"1}\"}",
		"",
		"data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"call_id\":\"call_1\",\"id\":\"fc_1\",\"name\":\"echo\"}}",
		"",
		"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":5,\"output_tokens\":3}}}",
		"",
	}, "\n")
	resp, err := codexConsumeSSE(strings.NewReader(payload), nil)
	if err != nil {
		t.Fatalf("consume sse failed: %v", err)
	}
	if resp.Content != "Hello" {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "echo" {
		t.Fatalf("unexpected tool name: %s", resp.ToolCalls[0].Name)
	}
	if strings.TrimSpace(string(resp.ToolCalls[0].Arguments)) != `{"a":1}` {
		t.Fatalf("unexpected tool args: %s", string(resp.ToolCalls[0].Arguments))
	}
	if resp.FinishReason != "stop" {
		t.Fatalf("unexpected finish reason: %s", resp.FinishReason)
	}
	if resp.Usage.PromptTokens != 5 || resp.Usage.CompletionTokens != 3 {
		t.Fatalf("unexpected usage: %#v", resp.Usage)
	}
}

func TestCodexConvertMessagesIncludesToolOutputs(t *testing.T) {
	system, input := codexConvertMessages([]Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hi"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1|fc_1", Name: "echo", Arguments: []byte(`{"q":"x"}`)}}},
		{Role: "tool", ToolCallID: "call_1|fc_1", Content: "done"},
	})
	if system != "sys" {
		t.Fatalf("unexpected system prompt: %q", system)
	}
	if len(input) < 3 {
		t.Fatalf("unexpected converted input length: %d", len(input))
	}
	last := input[len(input)-1]
	if last["type"] != "function_call_output" {
		t.Fatalf("expected function_call_output, got %#v", last)
	}
	if last["call_id"] != "call_1" {
		t.Fatalf("unexpected call id: %#v", last["call_id"])
	}
}
