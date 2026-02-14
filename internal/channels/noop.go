package channels

import (
	"context"
	"log"

	"github.com/grixate/squidbot/internal/agent"
)

type NoopAdapter struct {
	id  string
	log *log.Logger
}

func NewNoopAdapter(id string, logger *log.Logger) *NoopAdapter {
	if logger == nil {
		logger = log.Default()
	}
	return &NoopAdapter{id: id, log: logger}
}

func (a *NoopAdapter) ID() string { return a.id }

func (a *NoopAdapter) Start(_ context.Context) error { return nil }

func (a *NoopAdapter) Send(_ context.Context, msg agent.OutboundMessage) error {
	a.log.Printf("noop channel adapter=%s outbound chat_id=%s", a.id, msg.ChatID)
	return nil
}
