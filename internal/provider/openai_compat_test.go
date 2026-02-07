package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
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
