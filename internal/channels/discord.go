package channels

import (
	"bytes"
	"context"
	"crypto/ed25519"
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

type DiscordAdapter struct {
	cfg     config.GenericChannelConfig
	ingress IngressHandler
	log     *log.Logger
	client  *http.Client
	server  *http.Server
}

func NewDiscordAdapter(cfg config.GenericChannelConfig, ingress IngressHandler, logger *log.Logger) *DiscordAdapter {
	if logger == nil {
		logger = log.Default()
	}
	return &DiscordAdapter{
		cfg:     cfg,
		ingress: ingress,
		log:     logger,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (a *DiscordAdapter) ID() string { return "discord" }

func (a *DiscordAdapter) Start(ctx context.Context) error {
	listenAddr := metadataString(a.cfg, "listen_addr")
	if listenAddr == "" {
		return nil
	}
	path := firstNonEmpty(metadataString(a.cfg, "interactions_path"), "/channels/discord/interactions")
	mux := http.NewServeMux()
	mux.HandleFunc(path, a.handleInteraction)
	a.server = &http.Server{Addr: listenAddr, Handler: mux}
	if err := <-startHTTPServer(ctx, a.server); err != nil {
		return err
	}
	return nil
}

func (a *DiscordAdapter) Send(ctx context.Context, msg agent.OutboundMessage) error {
	token := strings.TrimSpace(a.cfg.Token)
	if token == "" {
		return fmt.Errorf("discord token is empty")
	}
	base := firstNonEmpty(metadataString(a.cfg, "api_base"), "https://discord.com/api/v10")
	endpoint := strings.TrimRight(base, "/") + "/channels/" + strings.TrimSpace(msg.ChatID) + "/messages"
	payload := map[string]any{"content": msg.Content}
	if strings.TrimSpace(msg.ReplyTo) != "" {
		payload["message_reference"] = map[string]any{"message_id": strings.TrimSpace(msg.ReplyTo)}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bot "+token)
	request.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("discord send failed status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (a *DiscordAdapter) handleInteraction(w http.ResponseWriter, r *http.Request) {
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
	var interaction struct {
		ID        string `json:"id"`
		Type      int    `json:"type"`
		ChannelID string `json:"channel_id"`
		Token     string `json:"token"`
		GuildID   string `json:"guild_id"`
		Data      struct {
			Name     string `json:"name"`
			CustomID string `json:"custom_id"`
			Options  []struct {
				Name  string `json:"name"`
				Value any    `json:"value"`
			} `json:"options"`
		} `json:"data"`
		User *struct {
			ID string `json:"id"`
		} `json:"user"`
		Member *struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"member"`
	}
	if err := json.Unmarshal(body, &interaction); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if interaction.Type == 1 {
		writeJSON(w, http.StatusOK, map[string]any{"type": 1})
		return
	}
	senderID := ""
	if interaction.Member != nil {
		senderID = strings.TrimSpace(interaction.Member.User.ID)
	}
	if senderID == "" && interaction.User != nil {
		senderID = strings.TrimSpace(interaction.User.ID)
	}
	content := strings.TrimSpace(buildDiscordInteractionText(interaction.Data.Name, interaction.Data.CustomID, interaction.Data.Options))
	if content == "" {
		content = "[interaction]"
	}
	chatID := strings.TrimSpace(interaction.ChannelID)
	if chatID == "" {
		chatID = strings.TrimSpace(interaction.GuildID)
	}
	message := agent.InboundMessage{
		RequestID: strings.TrimSpace(interaction.ID),
		SessionID: "discord:" + chatID,
		Channel:   "discord",
		ChatID:    chatID,
		SenderID:  senderID,
		Content:   content,
		Metadata: map[string]any{
			"interaction_id":    strings.TrimSpace(interaction.ID),
			"interaction_token": strings.TrimSpace(interaction.Token),
		},
		CreatedAt: time.Now().UTC(),
	}
	if a.ingress != nil {
		if err := a.ingress(r.Context(), message); err != nil {
			a.log.Printf("discord ingress failed: %v", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"type": 5})
}

func (a *DiscordAdapter) verifySignature(r *http.Request, body []byte) error {
	publicKeyHex := metadataString(a.cfg, "public_key")
	if publicKeyHex == "" {
		return nil
	}
	signatureHex := strings.TrimSpace(r.Header.Get("X-Signature-Ed25519"))
	timestamp := strings.TrimSpace(r.Header.Get("X-Signature-Timestamp"))
	if signatureHex == "" || timestamp == "" {
		return fmt.Errorf("missing discord signature headers")
	}
	publicKey, err := hex.DecodeString(publicKeyHex)
	if err != nil {
		return err
	}
	signature, err := hex.DecodeString(signatureHex)
	if err != nil {
		return err
	}
	message := append([]byte(timestamp), body...)
	if !ed25519.Verify(publicKey, message, signature) {
		return fmt.Errorf("invalid discord signature")
	}
	return nil
}

func buildDiscordInteractionText(name, customID string, options []struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}) string {
	if strings.TrimSpace(customID) != "" {
		return "interaction:" + strings.TrimSpace(customID)
	}
	if strings.TrimSpace(name) == "" {
		return ""
	}
	parts := []string{"/" + strings.TrimSpace(name)}
	for _, option := range options {
		optionName := strings.TrimSpace(option.Name)
		if optionName == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%v", optionName, option.Value))
	}
	return strings.Join(parts, " ")
}
