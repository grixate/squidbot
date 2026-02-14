package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/config"
)

type WebChatAdapter struct {
	cfg       config.GenericChannelConfig
	ingress   IngressHandler
	ask       AskHandler
	askStream AskStreamHandler
	log       *log.Logger
	client    *http.Client
	server    *http.Server

	mu      sync.RWMutex
	clients map[string]map[chan agent.StreamEvent]struct{}
}

func NewWebChatAdapter(cfg config.GenericChannelConfig, ingress IngressHandler, ask AskHandler, askStream AskStreamHandler, logger *log.Logger) *WebChatAdapter {
	if logger == nil {
		logger = log.Default()
	}
	return &WebChatAdapter{
		cfg:       cfg,
		ingress:   ingress,
		ask:       ask,
		askStream: askStream,
		log:       logger,
		client:    &http.Client{Timeout: 15 * time.Second},
		clients:   map[string]map[chan agent.StreamEvent]struct{}{},
	}
}

func (a *WebChatAdapter) ID() string { return "webchat" }

func (a *WebChatAdapter) Start(ctx context.Context) error {
	listenAddr := metadataString(a.cfg, "listen_addr")
	if listenAddr == "" {
		return nil
	}
	inboundPath := firstNonEmpty(metadataString(a.cfg, "inbound_path"), "/channels/webchat/inbound")
	streamPath := firstNonEmpty(metadataString(a.cfg, "stream_path"), "/channels/webchat/stream")
	mux := http.NewServeMux()
	mux.HandleFunc(inboundPath, a.handleInbound)
	mux.HandleFunc(streamPath, a.handleSSE)
	a.server = &http.Server{Addr: listenAddr, Handler: mux}
	if err := <-startHTTPServer(ctx, a.server); err != nil {
		return err
	}
	return nil
}

func (a *WebChatAdapter) Send(ctx context.Context, msg agent.OutboundMessage) error {
	event := agent.StreamEvent{Type: "final", Content: strings.TrimSpace(msg.Content), Done: true, Metadata: msg.Metadata}
	a.broadcast(strings.TrimSpace(msg.ChatID), event)
	if strings.TrimSpace(a.cfg.Endpoint) != "" {
		return a.sendWebhook(ctx, msg)
	}
	return nil
}

func (a *WebChatAdapter) SendStream(ctx context.Context, stream agent.OutboundStream) error {
	chatID := strings.TrimSpace(stream.ChatID)
	for _, event := range stream.Events {
		a.broadcast(chatID, event)
	}
	if strings.TrimSpace(a.cfg.Endpoint) == "" {
		return nil
	}
	final := ""
	for _, event := range stream.Events {
		if strings.TrimSpace(event.Content) != "" {
			final = event.Content
		}
		if strings.TrimSpace(event.Delta) != "" {
			final += event.Delta
		}
	}
	if strings.TrimSpace(final) == "" {
		return nil
	}
	return a.sendWebhook(ctx, agent.OutboundMessage{
		Channel:  "webchat",
		ChatID:   chatID,
		ReplyTo:  stream.ReplyTo,
		Content:  final,
		Metadata: stream.Metadata,
	})
}

func (a *WebChatAdapter) handleInbound(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := requireBearerAuth(r, a.cfg.AuthToken); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	var payload struct {
		RequestID string         `json:"request_id"`
		SessionID string         `json:"session_id"`
		ChatID    string         `json:"chat_id"`
		SenderID  string         `json:"sender_id"`
		Content   string         `json:"content"`
		Stream    bool           `json:"stream"`
		Metadata  map[string]any `json:"metadata"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	chatID := strings.TrimSpace(payload.ChatID)
	if chatID == "" {
		http.Error(w, "chat_id is required", http.StatusBadRequest)
		return
	}
	senderID := firstNonEmpty(payload.SenderID, "webchat-user")
	msg := agent.InboundMessage{
		RequestID: strings.TrimSpace(payload.RequestID),
		SessionID: firstNonEmpty(payload.SessionID, "webchat:"+chatID),
		Channel:   "webchat",
		ChatID:    chatID,
		SenderID:  senderID,
		Content:   strings.TrimSpace(payload.Content),
		Metadata:  payload.Metadata,
		CreatedAt: time.Now().UTC(),
	}
	if msg.Content == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}
	if payload.Stream && a.askStream != nil {
		err := a.askStream(r.Context(), msg, agent.StreamSinkFunc(func(ctx context.Context, event agent.StreamEvent) error {
			a.broadcast(chatID, event)
			return nil
		}))
		if err != nil {
			a.broadcast(chatID, agent.StreamEvent{Type: "error", Error: err.Error(), Done: true})
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "request_id": msg.RequestID, "stream": true})
		return
	}
	if a.ingress != nil {
		if err := a.ingress(r.Context(), msg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "request_id": msg.RequestID})
		return
	}
	if a.ask != nil {
		response, err := a.ask(r.Context(), msg)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		a.broadcast(chatID, agent.StreamEvent{Type: "final", Content: response, Done: true})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "response": response})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *WebChatAdapter) handleSSE(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := requireBearerAuth(r, a.cfg.AuthToken); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	chatID := strings.TrimSpace(r.URL.Query().Get("chat_id"))
	if chatID == "" {
		http.Error(w, "chat_id is required", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	client := make(chan agent.StreamEvent, 64)
	a.registerClient(chatID, client)
	defer a.unregisterClient(chatID, client)

	_, _ = io.WriteString(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = io.WriteString(w, ": keepalive\n\n")
			flusher.Flush()
		case event := <-client:
			payload, _ := json.Marshal(event)
			_, _ = fmt.Fprintf(w, "event: %s\n", firstNonEmpty(event.Type, "message"))
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func (a *WebChatAdapter) registerClient(chatID string, ch chan agent.StreamEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.clients[chatID] == nil {
		a.clients[chatID] = map[chan agent.StreamEvent]struct{}{}
	}
	a.clients[chatID][ch] = struct{}{}
}

func (a *WebChatAdapter) unregisterClient(chatID string, ch chan agent.StreamEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if clients, ok := a.clients[chatID]; ok {
		delete(clients, ch)
		if len(clients) == 0 {
			delete(a.clients, chatID)
		}
	}
	close(ch)
}

func (a *WebChatAdapter) broadcast(chatID string, event agent.StreamEvent) {
	a.mu.RLock()
	clients := a.clients[chatID]
	fallback := a.clients["*"]
	a.mu.RUnlock()
	for client := range clients {
		select {
		case client <- event:
		default:
		}
	}
	for client := range fallback {
		select {
		case client <- event:
		default:
		}
	}
}

func (a *WebChatAdapter) sendWebhook(ctx context.Context, msg agent.OutboundMessage) error {
	endpoint := strings.TrimSpace(a.cfg.Endpoint)
	if endpoint == "" {
		return nil
	}
	payload := map[string]any{
		"channel":  "webchat",
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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("webchat webhook failed status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
