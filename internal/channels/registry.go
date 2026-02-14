package channels

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/grixate/squidbot/internal/agent"
)

type Adapter interface {
	ID() string
	Start(ctx context.Context) error
	Send(ctx context.Context, msg agent.OutboundMessage) error
}

type StreamingAdapter interface {
	Adapter
	SendStream(ctx context.Context, stream agent.OutboundStream) error
}

type Registry struct {
	adapters map[string]Adapter
	log      *log.Logger
}

func NewRegistry(logger *log.Logger) *Registry {
	if logger == nil {
		logger = log.Default()
	}
	return &Registry{adapters: map[string]Adapter{}, log: logger}
}

func (r *Registry) Register(adapter Adapter) error {
	if r == nil {
		return fmt.Errorf("channel registry is nil")
	}
	if adapter == nil {
		return fmt.Errorf("channel adapter is nil")
	}
	id := strings.ToLower(strings.TrimSpace(adapter.ID()))
	if id == "" {
		return fmt.Errorf("channel adapter id is empty")
	}
	if _, exists := r.adapters[id]; exists {
		return fmt.Errorf("channel adapter %q already registered", id)
	}
	r.adapters[id] = adapter
	return nil
}

func (r *Registry) StartAll(ctx context.Context) {
	if r == nil {
		return
	}
	for id, adapter := range r.adapters {
		id := id
		adapter := adapter
		go func() {
			if err := adapter.Start(ctx); err != nil {
				r.log.Printf("channel %s stopped: %v", id, err)
			}
		}()
	}
}

func (r *Registry) Send(ctx context.Context, msg agent.OutboundMessage) error {
	if r == nil {
		return fmt.Errorf("channel registry is nil")
	}
	id := strings.ToLower(strings.TrimSpace(msg.Channel))
	adapter, ok := r.adapters[id]
	if !ok {
		return fmt.Errorf("channel %q is not configured", id)
	}
	return adapter.Send(ctx, msg)
}

func (r *Registry) SendStream(ctx context.Context, stream agent.OutboundStream) error {
	if r == nil {
		return fmt.Errorf("channel registry is nil")
	}
	id := strings.ToLower(strings.TrimSpace(stream.Channel))
	adapter, ok := r.adapters[id]
	if !ok {
		return fmt.Errorf("channel %q is not configured", id)
	}
	if streamingAdapter, ok := adapter.(StreamingAdapter); ok {
		return streamingAdapter.SendStream(ctx, stream)
	}
	finalContent := ""
	if len(stream.Events) > 0 {
		for _, event := range stream.Events {
			if strings.TrimSpace(event.Content) != "" {
				finalContent = event.Content
				break
			}
			if strings.TrimSpace(event.Delta) != "" {
				finalContent += event.Delta
			}
		}
	}
	if strings.TrimSpace(finalContent) == "" {
		return nil
	}
	return adapter.Send(ctx, agent.OutboundMessage{
		Channel:  stream.Channel,
		ChatID:   stream.ChatID,
		ReplyTo:  stream.ReplyTo,
		Content:  finalContent,
		Metadata: stream.Metadata,
	})
}

func (r *Registry) IDs() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.adapters))
	for id := range r.adapters {
		out = append(out, id)
	}
	return out
}
