package channels

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"testing"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/config"
)

type stubAdapter struct {
	id      string
	sent    []agent.OutboundMessage
	startFn func(context.Context) error
	sendErr error
}

func (a *stubAdapter) ID() string { return a.id }
func (a *stubAdapter) Start(ctx context.Context) error {
	if a.startFn != nil {
		return a.startFn(ctx)
	}
	return nil
}
func (a *stubAdapter) Send(ctx context.Context, msg agent.OutboundMessage) error {
	if a.sendErr != nil {
		return a.sendErr
	}
	a.sent = append(a.sent, msg)
	return nil
}

func TestRegistryRegisterSendAndSendStreamFallback(t *testing.T) {
	r := NewRegistry(log.New(io.Discard, "", 0))
	if err := r.Register(nil); err == nil {
		t.Fatal("expected nil adapter error")
	}
	adapter := &stubAdapter{id: "webchat"}
	if err := r.Register(adapter); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(adapter); err == nil {
		t.Fatal("expected duplicate register error")
	}

	if err := r.Send(context.Background(), agent.OutboundMessage{Channel: "missing"}); err == nil {
		t.Fatal("expected missing channel error")
	}
	if err := r.Send(context.Background(), agent.OutboundMessage{Channel: "webchat", ChatID: "c1", Content: "hello"}); err != nil {
		t.Fatal(err)
	}

	stream := agent.OutboundStream{Channel: "webchat", ChatID: "c1", Events: []agent.StreamEvent{{Delta: "hel"}, {Delta: "lo"}}}
	if err := r.SendStream(context.Background(), stream); err != nil {
		t.Fatal(err)
	}
	if len(adapter.sent) != 2 {
		t.Fatalf("expected 2 outbound sends, got %d", len(adapter.sent))
	}
	if adapter.sent[1].Content != "hello" {
		t.Fatalf("expected stream fallback message content, got %q", adapter.sent[1].Content)
	}

	ids := r.IDs()
	if len(ids) != 1 || ids[0] != "webchat" {
		t.Fatalf("unexpected ids: %+v", ids)
	}
}

func TestRegistrySendStreamHandlesEmptyFinal(t *testing.T) {
	r := NewRegistry(log.New(io.Discard, "", 0))
	adapter := &stubAdapter{id: "noop"}
	if err := r.Register(adapter); err != nil {
		t.Fatal(err)
	}
	stream := agent.OutboundStream{Channel: "noop", ChatID: "c1", Events: []agent.StreamEvent{{Type: "meta"}}}
	if err := r.SendStream(context.Background(), stream); err != nil {
		t.Fatal(err)
	}
	if len(adapter.sent) != 0 {
		t.Fatalf("expected no fallback send, got %d", len(adapter.sent))
	}
}

func TestHelpersAndNoopAdapter(t *testing.T) {
	cfg := config.GenericChannelConfig{Metadata: map[string]string{"enabled": "true", "name": " x "}}
	if !metadataBool(cfg, "enabled") {
		t.Fatal("expected metadataBool true")
	}
	if metadataBool(config.GenericChannelConfig{Metadata: map[string]string{"enabled": "bad"}}, "enabled") {
		t.Fatal("expected metadataBool false on invalid value")
	}
	if firstNonEmpty("", "  ", "ok") != "ok" {
		t.Fatal("unexpected firstNonEmpty result")
	}

	req := &http.Request{Header: http.Header{}}
	if err := requireBearerAuth(req, ""); err != nil {
		t.Fatalf("unexpected auth error: %v", err)
	}
	if err := requireBearerAuth(req, "token"); err == nil {
		t.Fatal("expected unauthorized error")
	}
	req.Header.Set("Authorization", "Bearer token")
	if err := requireBearerAuth(req, "token"); err != nil {
		t.Fatalf("unexpected auth error: %v", err)
	}

	noop := NewNoopAdapter("noop", log.New(io.Discard, "", 0))
	if noop.ID() != "noop" {
		t.Fatalf("unexpected noop id: %s", noop.ID())
	}
	if err := noop.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := noop.Send(context.Background(), agent.OutboundMessage{ChatID: "c1"}); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryPropagatesAdapterErrors(t *testing.T) {
	r := NewRegistry(log.New(io.Discard, "", 0))
	adapter := &stubAdapter{id: "a", sendErr: errors.New("boom")}
	if err := r.Register(adapter); err != nil {
		t.Fatal(err)
	}
	if err := r.Send(context.Background(), agent.OutboundMessage{Channel: "a"}); err == nil || err.Error() != "boom" {
		t.Fatalf("expected adapter send error, got %v", err)
	}
}
