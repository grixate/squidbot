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
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/config"
)

type WhatsAppAdapter struct {
	cfg     config.GenericChannelConfig
	ingress IngressHandler
	log     *log.Logger
	client  *http.Client
	server  *http.Server
}

func NewWhatsAppAdapter(cfg config.GenericChannelConfig, ingress IngressHandler, logger *log.Logger) *WhatsAppAdapter {
	if logger == nil {
		logger = log.Default()
	}
	return &WhatsAppAdapter{
		cfg:     cfg,
		ingress: ingress,
		log:     logger,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (a *WhatsAppAdapter) ID() string { return "whatsapp" }

func (a *WhatsAppAdapter) Start(ctx context.Context) error {
	listenAddr := metadataString(a.cfg, "listen_addr")
	if listenAddr == "" {
		return nil
	}
	path := firstNonEmpty(metadataString(a.cfg, "webhook_path"), "/channels/whatsapp/webhook")
	mux := http.NewServeMux()
	mux.HandleFunc(path, a.handleWebhook)
	a.server = &http.Server{Addr: listenAddr, Handler: mux}
	if err := <-startHTTPServer(ctx, a.server); err != nil {
		return err
	}
	return nil
}

func (a *WhatsAppAdapter) Send(ctx context.Context, msg agent.OutboundMessage) error {
	token := strings.TrimSpace(a.cfg.Token)
	if token == "" {
		return fmt.Errorf("whatsapp token is empty")
	}
	phoneNumberID := strings.TrimSpace(metadataString(a.cfg, "phone_number_id"))
	if phoneNumberID == "" && msg.Metadata != nil {
		if value, ok := msg.Metadata["phone_number_id"].(string); ok {
			phoneNumberID = strings.TrimSpace(value)
		}
	}
	if phoneNumberID == "" {
		return fmt.Errorf("whatsapp phone_number_id is empty")
	}
	apiBase := firstNonEmpty(metadataString(a.cfg, "api_base"), "https://graph.facebook.com")
	apiVersion := firstNonEmpty(metadataString(a.cfg, "api_version"), "v20.0")
	endpoint := strings.TrimRight(apiBase, "/") + "/" + strings.TrimSpace(apiVersion) + "/" + phoneNumberID + "/messages"
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"to":                strings.TrimSpace(msg.ChatID),
		"type":              "text",
		"text": map[string]any{
			"body": msg.Content,
		},
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
		return fmt.Errorf("whatsapp send failed status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (a *WhatsAppAdapter) handleWebhook(w http.ResponseWriter, r *http.Request) {
	verifyToken := metadataString(a.cfg, "verify_token")
	if r.Method == http.MethodGet {
		mode := strings.TrimSpace(r.URL.Query().Get("hub.mode"))
		token := strings.TrimSpace(r.URL.Query().Get("hub.verify_token"))
		challenge := strings.TrimSpace(r.URL.Query().Get("hub.challenge"))
		if mode == "subscribe" && verifyToken != "" && token == verifyToken && challenge != "" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(challenge))
			return
		}
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
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
		Entry []struct {
			ID      string `json:"id"`
			Changes []struct {
				Field string `json:"field"`
				Value struct {
					Metadata struct {
						PhoneNumberID string `json:"phone_number_id"`
					} `json:"metadata"`
					Messages []struct {
						ID        string `json:"id"`
						From      string `json:"from"`
						Timestamp string `json:"timestamp"`
						Type      string `json:"type"`
						Text      struct {
							Body string `json:"body"`
						} `json:"text"`
					} `json:"messages"`
				} `json:"value"`
			} `json:"changes"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	for _, entry := range envelope.Entry {
		for _, change := range entry.Changes {
			for _, incoming := range change.Value.Messages {
				content := strings.TrimSpace(incoming.Text.Body)
				if content == "" {
					content = "[" + firstNonEmpty(strings.TrimSpace(incoming.Type), "message") + "]"
				}
				msg := agent.InboundMessage{
					RequestID: strings.TrimSpace(incoming.ID),
					SessionID: "whatsapp:" + strings.TrimSpace(incoming.From),
					Channel:   "whatsapp",
					ChatID:    strings.TrimSpace(incoming.From),
					SenderID:  strings.TrimSpace(incoming.From),
					Content:   content,
					Metadata: map[string]any{
						"phone_number_id": strings.TrimSpace(change.Value.Metadata.PhoneNumberID),
						"timestamp":       strings.TrimSpace(incoming.Timestamp),
					},
					CreatedAt: time.Now().UTC(),
				}
				if a.ingress != nil {
					if err := a.ingress(r.Context(), msg); err != nil {
						a.log.Printf("whatsapp ingress failed: %v", err)
					}
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *WhatsAppAdapter) verifySignature(r *http.Request, body []byte) error {
	secret := metadataString(a.cfg, "app_secret")
	if secret == "" {
		return nil
	}
	received := strings.TrimSpace(r.Header.Get("X-Hub-Signature-256"))
	if received == "" {
		return fmt.Errorf("missing whatsapp signature")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(received)) {
		return fmt.Errorf("whatsapp signature mismatch")
	}
	return nil
}
