package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/config"
)

type WebhookAdapter struct {
	id     string
	cfg    config.GenericChannelConfig
	client *http.Client
	log    *log.Logger
}

func NewWebhookAdapter(id string, cfg config.GenericChannelConfig, logger *log.Logger) *WebhookAdapter {
	if logger == nil {
		logger = log.Default()
	}
	return &WebhookAdapter{
		id:  strings.ToLower(strings.TrimSpace(id)),
		cfg: cfg,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		log: logger,
	}
}

func (a *WebhookAdapter) ID() string { return a.id }

func (a *WebhookAdapter) Start(_ context.Context) error { return nil }

func (a *WebhookAdapter) Send(ctx context.Context, msg agent.OutboundMessage) error {
	endpoint := strings.TrimSpace(a.cfg.Endpoint)
	if endpoint == "" {
		return fmt.Errorf("channel %q endpoint is empty", a.id)
	}
	payload := map[string]any{
		"channel":  a.id,
		"chat_id":  msg.ChatID,
		"content":  msg.Content,
		"reply_to": msg.ReplyTo,
		"metadata": msg.Metadata,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(a.cfg.AuthToken); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range a.cfg.Headers {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			request.Header.Set(key, value)
		}
	}
	resp, err := a.client.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("channel %q webhook returned %d", a.id, resp.StatusCode)
	}
	return nil
}
