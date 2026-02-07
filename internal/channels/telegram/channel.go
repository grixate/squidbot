package telegram

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/config"
)

type IngressHandler func(ctx context.Context, msg agent.InboundMessage) error

type Channel struct {
	cfg     config.TelegramConfig
	bot     *tgbotapi.BotAPI
	ingress IngressHandler
	log     *log.Logger
}

func New(cfg config.TelegramConfig, ingress IngressHandler, logger *log.Logger) *Channel {
	if logger == nil {
		logger = log.Default()
	}
	return &Channel{cfg: cfg, ingress: ingress, log: logger}
}

func (c *Channel) Start(ctx context.Context) error {
	if strings.TrimSpace(c.cfg.Token) == "" {
		return fmt.Errorf("telegram token not configured")
	}
	bot, err := tgbotapi.NewBotAPI(c.cfg.Token)
	if err != nil {
		return err
	}
	c.bot = bot
	updatesCfg := tgbotapi.NewUpdate(0)
	updatesCfg.Timeout = 60
	updates := bot.GetUpdatesChan(updatesCfg)

	for {
		select {
		case <-ctx.Done():
			bot.StopReceivingUpdates()
			return nil
		case update := <-updates:
			if update.Message == nil || update.Message.From == nil {
				continue
			}
			if !c.allowed(update.Message.From.ID, update.Message.From.UserName) {
				continue
			}

			msg := c.toInbound(update)
			if c.ingress != nil {
				if err := c.ingress(ctx, msg); err != nil {
					c.log.Printf("telegram ingress failed: %v", err)
				}
			}
		}
	}
}

func (c *Channel) Send(ctx context.Context, msg agent.OutboundMessage) error {
	if c.bot == nil {
		return fmt.Errorf("telegram channel not running")
	}
	chatID, err := strconv.ParseInt(msg.ChatID, 10, 64)
	if err != nil {
		return err
	}
	telegramMsg := tgbotapi.NewMessage(chatID, msg.Content)
	_, err = c.bot.Send(telegramMsg)
	return err
}

func (c *Channel) allowed(userID int64, username string) bool {
	if len(c.cfg.AllowFrom) == 0 {
		return true
	}
	userIDStr := strconv.FormatInt(userID, 10)
	username = strings.ToLower(strings.TrimPrefix(username, "@"))
	for _, allowed := range c.cfg.AllowFrom {
		candidate := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(allowed), "@"))
		if candidate == userIDStr || (username != "" && candidate == username) {
			return true
		}
	}
	return false
}

func (c *Channel) toInbound(update tgbotapi.Update) agent.InboundMessage {
	m := update.Message
	content := strings.TrimSpace(m.Text)
	if content == "" {
		content = strings.TrimSpace(m.Caption)
	}
	metadata := map[string]interface{}{
		"telegram_message_id": m.MessageID,
		"is_group":            m.Chat.IsGroup() || m.Chat.IsSuperGroup(),
	}
	media := []string{}
	if len(m.Photo) > 0 {
		metadata["photo_file_id"] = m.Photo[len(m.Photo)-1].FileID
		media = append(media, m.Photo[len(m.Photo)-1].FileID)
		if content == "" {
			content = "[photo]"
		}
	}
	if m.Document != nil {
		metadata["document_file_id"] = m.Document.FileID
		metadata["document_name"] = m.Document.FileName
		media = append(media, m.Document.FileID)
		if content == "" {
			content = "[document]"
		}
	}
	if m.Audio != nil {
		metadata["audio_file_id"] = m.Audio.FileID
		media = append(media, m.Audio.FileID)
		if content == "" {
			content = "[audio]"
		}
	}
	if m.Voice != nil {
		metadata["voice_file_id"] = m.Voice.FileID
		media = append(media, m.Voice.FileID)
		if content == "" {
			content = "[voice]"
		}
	}
	if content == "" {
		content = "[empty message]"
	}

	return agent.InboundMessage{
		RequestID: fmt.Sprintf("telegram-%d-%d", m.Chat.ID, m.MessageID),
		SessionID: fmt.Sprintf("telegram:%d", m.Chat.ID),
		Channel:   "telegram",
		ChatID:    strconv.FormatInt(m.Chat.ID, 10),
		SenderID:  strconv.FormatInt(m.From.ID, 10),
		Content:   content,
		Media:     media,
		Metadata:  metadata,
		CreatedAt: time.Now().UTC(),
	}
}
