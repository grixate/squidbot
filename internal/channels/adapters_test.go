package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/config"
)

func TestSlackAdapterSend(t *testing.T) {
	called := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("failed parsing payload: %v", err)
		}
		if payload["channel"] != "C1" {
			t.Fatalf("unexpected channel: %#v", payload["channel"])
		}
		if payload["text"] != "hello" {
			t.Fatalf("unexpected text: %#v", payload["text"])
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer ts.Close()

	adapter := NewSlackAdapter(config.GenericChannelConfig{
		Token:    "test-token",
		Endpoint: ts.URL,
	}, nil, log.New(io.Discard, "", 0))
	if err := adapter.Send(context.Background(), agent.OutboundMessage{ChatID: "C1", Content: "hello"}); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if !called {
		t.Fatalf("expected send endpoint to be called")
	}
}

func TestSlackAdapterInboundMessage(t *testing.T) {
	var inbound agent.InboundMessage
	adapter := NewSlackAdapter(config.GenericChannelConfig{}, func(ctx context.Context, msg agent.InboundMessage) error {
		inbound = msg
		return nil
	}, log.New(io.Discard, "", 0))

	payload := `{"type":"event_callback","event_id":"Ev1","event":{"type":"message","user":"U1","channel":"C1","text":"ping","ts":"1710000000.100","thread_ts":"1710000000.100"}}`
	req := httptest.NewRequest(http.MethodPost, "/channels/slack/events", strings.NewReader(payload))
	w := httptest.NewRecorder()
	adapter.handleEvent(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", w.Code)
	}
	if inbound.Channel != "slack" || inbound.ChatID != "C1" || inbound.SenderID != "U1" {
		t.Fatalf("unexpected inbound message: %#v", inbound)
	}
}

func TestDiscordAdapterSend(t *testing.T) {
	called := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if got := r.Header.Get("Authorization"); got != "Bot token-1" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		if !strings.HasSuffix(r.URL.Path, "/channels/123/messages") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	adapter := NewDiscordAdapter(config.GenericChannelConfig{
		Token: "token-1",
		Metadata: map[string]string{
			"api_base": ts.URL,
		},
	}, nil, log.New(io.Discard, "", 0))
	if err := adapter.Send(context.Background(), agent.OutboundMessage{ChatID: "123", Content: "hi"}); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if !called {
		t.Fatalf("expected discord endpoint to be called")
	}
}

func TestDiscordAdapterInboundInteraction(t *testing.T) {
	var inbound agent.InboundMessage
	adapter := NewDiscordAdapter(config.GenericChannelConfig{}, func(ctx context.Context, msg agent.InboundMessage) error {
		inbound = msg
		return nil
	}, log.New(io.Discard, "", 0))
	payload := `{"id":"itx-1","type":2,"channel_id":"chan-9","data":{"name":"ask","options":[{"name":"q","value":"hello"}]},"member":{"user":{"id":"user-1"}}}`
	req := httptest.NewRequest(http.MethodPost, "/channels/discord/interactions", strings.NewReader(payload))
	w := httptest.NewRecorder()
	adapter.handleInteraction(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", w.Code)
	}
	if inbound.Channel != "discord" || inbound.ChatID != "chan-9" || inbound.SenderID != "user-1" {
		t.Fatalf("unexpected inbound message: %#v", inbound)
	}
	if !strings.Contains(inbound.Content, "/ask") {
		t.Fatalf("unexpected content: %q", inbound.Content)
	}
}

func TestWebChatAdapterHandleInboundStream(t *testing.T) {
	events := []agent.StreamEvent{}
	adapter := NewWebChatAdapter(config.GenericChannelConfig{}, nil, nil, func(ctx context.Context, msg agent.InboundMessage, sink agent.StreamSink) error {
		_ = sink.OnEvent(ctx, agent.StreamEvent{Type: "assistant_delta", Delta: "hel"})
		_ = sink.OnEvent(ctx, agent.StreamEvent{Type: "final", Content: "hello", Done: true})
		return nil
	}, log.New(io.Discard, "", 0))
	ch := make(chan agent.StreamEvent, 8)
	adapter.registerClient("chat-1", ch)
	defer adapter.unregisterClient("chat-1", ch)

	req := httptest.NewRequest(http.MethodPost, "/channels/webchat/inbound", strings.NewReader(`{"chat_id":"chat-1","sender_id":"u1","content":"hello?","stream":true}`))
	w := httptest.NewRecorder()
	adapter.handleInbound(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", w.Code)
	}
	deadline := time.After(2 * time.Second)
	for len(events) < 2 {
		select {
		case ev := <-ch:
			events = append(events, ev)
		case <-deadline:
			t.Fatalf("timed out waiting for events")
		}
	}
	if events[1].Content != "hello" {
		t.Fatalf("unexpected final event: %#v", events[1])
	}
}

func TestWhatsAppAdapterSend(t *testing.T) {
	called := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		if !strings.Contains(r.URL.Path, "/v20.0/999/messages") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	adapter := NewWhatsAppAdapter(config.GenericChannelConfig{
		Token: "token",
		Metadata: map[string]string{
			"api_base":        ts.URL,
			"api_version":     "v20.0",
			"phone_number_id": "999",
		},
	}, nil, log.New(io.Discard, "", 0))
	if err := adapter.Send(context.Background(), agent.OutboundMessage{ChatID: "15550001234", Content: "hello"}); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if !called {
		t.Fatalf("expected whatsapp endpoint to be called")
	}
}

func TestWhatsAppAdapterInboundWebhook(t *testing.T) {
	var inbound agent.InboundMessage
	adapter := NewWhatsAppAdapter(config.GenericChannelConfig{}, func(ctx context.Context, msg agent.InboundMessage) error {
		inbound = msg
		return nil
	}, log.New(io.Discard, "", 0))
	payload := `{"entry":[{"changes":[{"value":{"metadata":{"phone_number_id":"999"},"messages":[{"id":"wamid-1","from":"15550001234","type":"text","text":{"body":"hi"}}]}}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/channels/whatsapp/webhook", strings.NewReader(payload))
	w := httptest.NewRecorder()
	adapter.handleWebhook(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", w.Code)
	}
	if inbound.Channel != "whatsapp" || inbound.ChatID != "15550001234" {
		t.Fatalf("unexpected inbound message: %#v", inbound)
	}
}

func TestWhatsAppAdapterVerification(t *testing.T) {
	adapter := NewWhatsAppAdapter(config.GenericChannelConfig{Metadata: map[string]string{"verify_token": "abc"}}, nil, log.New(io.Discard, "", 0))
	url := fmt.Sprintf("/channels/whatsapp/webhook?hub.mode=subscribe&hub.verify_token=%s&hub.challenge=123", "abc")
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	adapter.handleWebhook(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", w.Code)
	}
	if strings.TrimSpace(w.Body.String()) != "123" {
		t.Fatalf("unexpected challenge body: %q", w.Body.String())
	}
}
