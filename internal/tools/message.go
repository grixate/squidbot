package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type SendMessageFunc func(ctx context.Context, channel, chatID, content string) error

type MessageTool struct {
	send      SendMessageFunc
	channel   string
	chatID    string
	sessionID string
}

func NewMessageTool(send SendMessageFunc) *MessageTool {
	return &MessageTool{send: send}
}

func (t *MessageTool) SetContext(channel, chatID, sessionID string) {
	t.channel = channel
	t.chatID = chatID
	t.sessionID = sessionID
}

func (t *MessageTool) Name() string { return "message" }
func (t *MessageTool) Description() string {
	return "Send a message to a user on a target channel."
}
func (t *MessageTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"content": map[string]any{"type": "string"},
		"channel": map[string]any{"type": "string"},
		"chat_id": map[string]any{"type": "string"},
	}, "required": []string{"content"}}
}
func (t *MessageTool) Execute(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	if t.send == nil {
		return ToolResult{}, errors.New("message sending not configured")
	}
	var in struct {
		Content string `json:"content"`
		Channel string `json:"channel"`
		ChatID  string `json:"chat_id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(in.Content) == "" {
		return ToolResult{}, errors.New("content is required")
	}
	channel := t.channel
	if strings.TrimSpace(in.Channel) != "" {
		channel = in.Channel
	}
	chatID := t.chatID
	if strings.TrimSpace(in.ChatID) != "" {
		chatID = in.ChatID
	}
	if strings.TrimSpace(channel) == "" || strings.TrimSpace(chatID) == "" {
		return ToolResult{Text: "Error: No target channel/chat specified"}, nil
	}
	if err := t.send(ctx, channel, chatID, in.Content); err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Text: fmt.Sprintf("Message sent to %s:%s", channel, chatID)}, nil
}
