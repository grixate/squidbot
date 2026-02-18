package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAICompatAuthorizationHeader(t *testing.T) {
	t.Run("omits authorization header when key is empty", func(t *testing.T) {
		var authHeader string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
		}))
		defer server.Close()

		p := NewOpenAICompatProvider("", server.URL+"/v1")
		_, err := p.Chat(context.Background(), ChatRequest{
			Model:    "test-model",
			Messages: []Message{{Role: "user", Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if authHeader != "" {
			t.Fatalf("expected empty authorization header, got %q", authHeader)
		}
	})

	t.Run("sets authorization header when key is provided", func(t *testing.T) {
		var authHeader string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
		}))
		defer server.Close()

		p := NewOpenAICompatProvider("secret-key", server.URL+"/v1")
		_, err := p.Chat(context.Background(), ChatRequest{
			Model:    "test-model",
			Messages: []Message{{Role: "user", Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if authHeader != "Bearer secret-key" {
			t.Fatalf("expected bearer auth header, got %q", authHeader)
		}
	})
}

func TestOpenAICompatCustomAuthHeaderAndPrefix(t *testing.T) {
	var gotAuth string
	var gotTrace string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("X-API-Key")
		gotTrace = r.Header.Get("X-Trace")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	p := NewOpenAICompatProviderWithOptions("secret", server.URL+"/v1", "X-API-Key", "Token ", map[string]string{"X-Trace": "trace-1"})
	_, err := p.Chat(context.Background(), ChatRequest{Model: "test-model", Messages: []Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Token secret" {
		t.Fatalf("unexpected custom auth header: %q", gotAuth)
	}
	if gotTrace != "trace-1" {
		t.Fatalf("unexpected custom header: %q", gotTrace)
	}
}

func TestOpenAICompatHandlesNon2xxAndNoChoices(t *testing.T) {
	t.Run("returns detailed non-2xx error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad request"}`))
		}))
		defer server.Close()

		p := NewOpenAICompatProvider("key", server.URL+"/v1")
		_, err := p.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "hello"}}})
		if err == nil || !strings.Contains(err.Error(), "provider http 400") {
			t.Fatalf("expected 400 provider error, got %v", err)
		}
	})

	t.Run("returns error when choices are missing", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
		}))
		defer server.Close()

		p := NewOpenAICompatProvider("key", server.URL+"/v1")
		_, err := p.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "hello"}}})
		if err == nil || err.Error() != "provider returned no choices" {
			t.Fatalf("expected no-choices error, got %v", err)
		}
	})
}

func TestOpenAICompatMapsToolCallsAndUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices":[{
				"finish_reason":"tool_calls",
				"message":{
					"content":"",
					"tool_calls":[
						{"id":"tc-1","function":{"name":"spawn","arguments":"{\"task\":\"run\"}"}},
						{"id":"tc-2","function":{"name":"status","arguments":""}}
					]
				}
			}],
			"usage":{"prompt_tokens":11,"completion_tokens":22,"total_tokens":33}
		}`))
	}))
	defer server.Close()

	p := NewOpenAICompatProvider("key", server.URL+"/v1")
	resp, err := p.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(resp.ToolCalls))
	}
	if string(resp.ToolCalls[0].Arguments) != `{"task":"run"}` {
		t.Fatalf("unexpected first tool args: %s", string(resp.ToolCalls[0].Arguments))
	}
	if string(resp.ToolCalls[1].Arguments) != `{}` {
		t.Fatalf("expected default empty args object, got %s", string(resp.ToolCalls[1].Arguments))
	}
	if resp.Usage.TotalTokens != 33 {
		t.Fatalf("unexpected usage mapping: %+v", resp.Usage)
	}
}

func TestOpenAICompatStreamFallbackEventSequence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices":[{
				"finish_reason":"tool_calls",
				"message":{
					"content":"hello",
					"tool_calls":[{"id":"tc-1","function":{"name":"spawn","arguments":"{\"task\":\"run\"}"}}]
				}
			}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`))
	}))
	defer server.Close()

	p := NewOpenAICompatProvider("key", server.URL+"/v1")
	events, errs := p.Stream(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "hello"}}})

	var seq []string
	for events != nil || errs != nil {
		select {
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if ev.DeltaContent != "" {
				seq = append(seq, "delta:"+ev.DeltaContent)
			}
			if ev.ToolCall != nil {
				seq = append(seq, "tool:"+ev.ToolCall.Name)
			}
			if ev.Done {
				seq = append(seq, "done")
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				t.Fatalf("unexpected stream error: %v", err)
			}
		}
	}

	joined := strings.Join(seq, ",")
	if joined != "delta:hello,tool:spawn,done" {
		t.Fatalf("unexpected stream sequence: %s", joined)
	}
}
