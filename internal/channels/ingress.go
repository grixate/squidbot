package channels

import (
	"context"

	"github.com/grixate/squidbot/internal/agent"
)

type IngressHandler func(ctx context.Context, msg agent.InboundMessage) error

type AskHandler func(ctx context.Context, msg agent.InboundMessage) (string, error)

type AskStreamHandler func(ctx context.Context, msg agent.InboundMessage, sink agent.StreamSink) error
