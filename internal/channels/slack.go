package channels

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/config"
)

type SlackAdapter struct {
	cfg     config.GenericChannelConfig
	ingress IngressHandler
	log     *log.Logger
	client  *http.Client
	server  *http.Server
}

func NewSlackAdapter(cfg config.GenericChannelConfig, ingress IngressHandler, logger *log.Logger) *SlackAdapter {
	if logger == nil {
		logger = log.Default()
	}
	return &SlackAdapter{
		cfg:     cfg,
		ingress: ingress,
		log:     logger,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (a *SlackAdapter) ID() string { return "slack" }

func (a *SlackAdapter) Start(ctx context.Context) error {
	listenAddr := metadataString(a.cfg, "listen_addr")
	if listenAddr == "" {
		return nil
	}
	if metadataBool(a.cfg, "socket_mode") {
		a.log.Printf("slack adapter socket_mode=true requested but not supported; using Events API listener")
	}
	path := firstNonEmpty(metadataString(a.cfg, "events_path"), "/channels/slack/events")
	mux := http.NewServeMux()
	mux.HandleFunc(path, a.handleEvent)
	a.server = &http.Server{Addr: listenAddr, Handler: mux}
	if err := <-startHTTPServer(ctx, a.server); err != nil {
		return err
	}
	return nil
}

func (a *SlackAdapter) Send(ctx context.Context, msg agent.OutboundMessage) error {
	token := strings.TrimSpace(a.cfg.Token)
	if token == "" {
		return fmt.Errorf("slack token is empty")
	}
	endpoint := firstNonEmpty(a.cfg.Endpoint, "https://slack.com/api/chat.postMessage")
	threadTS := strings.TrimSpace(msg.ReplyTo)
	if threadTS == "" && msg.Metadata != nil {
		if value, ok := msg.Metadata["thread_ts"].(string); ok {
			threadTS = strings.TrimSpace(value)
		}
	}
	payload := map[string]any{
		"channel": msg.ChatID,
		"text":    msg.Content,
	}
	if threadTS != "" {
		payload["thread_ts"] = threadTS
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("slack send failed status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var envelope struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil
	}
	if !envelope.OK {
		return fmt.Errorf("slack send failed: %s", strings.TrimSpace(envelope.Error))
	}
	return nil
}

func (a *SlackAdapter) handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := a.verifySignature(r, body); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var envelope struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		EventID   string `json:"event_id"`
		Event     struct {
			Type     string `json:"type"`
			SubType  string `json:"subtype"`
			BotID    string `json:"bot_id"`
			Text     string `json:"text"`
			User     string `json:"user"`
			Channel  string `json:"channel"`
			TS       string `json:"ts"`
			ThreadTS string `json:"thread_ts"`
		} `json:"event"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if envelope.Type == "url_verification" {
		writeJSON(w, http.StatusOK, map[string]any{"challenge": envelope.Challenge})
		return
	}
	if envelope.Type != "event_callback" || envelope.Event.Type != "message" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if strings.TrimSpace(envelope.Event.BotID) != "" || strings.EqualFold(strings.TrimSpace(envelope.Event.SubType), "bot_message") {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	content := strings.TrimSpace(envelope.Event.Text)
	if content == "" {
		content = "[empty message]"
	}
	threadTS := strings.TrimSpace(envelope.Event.ThreadTS)
	if threadTS == "" {
		threadTS = strings.TrimSpace(envelope.Event.TS)
	}
	message := agent.InboundMessage{
		RequestID: firstNonEmpty(strings.TrimSpace(envelope.EventID), "slack:"+threadTS),
		SessionID: "slack:" + strings.TrimSpace(envelope.Event.Channel),
		Channel:   "slack",
		ChatID:    strings.TrimSpace(envelope.Event.Channel),
		SenderID:  strings.TrimSpace(envelope.Event.User),
		Content:   content,
		Metadata: map[string]any{
			"thread_ts": threadTS,
			"ts":        strings.TrimSpace(envelope.Event.TS),
		},
		CreatedAt: time.Now().UTC(),
	}
	if a.ingress != nil {
		if err := a.ingress(r.Context(), message); err != nil {
			a.log.Printf("slack ingress failed: %v", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *SlackAdapter) verifySignature(r *http.Request, body []byte) error {
	secret := metadataString(a.cfg, "signing_secret")
	if secret == "" {
		return nil
	}
	timestamp := strings.TrimSpace(r.Header.Get("X-Slack-Request-Timestamp"))
	received := strings.TrimSpace(r.Header.Get("X-Slack-Signature"))
	if timestamp == "" || received == "" {
		return fmt.Errorf("missing slack signature headers")
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	if now-ts > 300 || ts-now > 300 {
		return fmt.Errorf("slack signature timestamp out of range")
	}
	base := []byte("v0:" + timestamp + ":" + string(body))
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(base)
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(received)) {
		return fmt.Errorf("slack signature mismatch")
	}
	return nil
}
