package telegram

import (
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/grixate/squidbot/internal/config"
)

func TestChannelAllowed(t *testing.T) {
	c := &Channel{cfg: config.TelegramConfig{AllowFrom: []string{"123", "@CaseUser"}}}

	if !c.allowed(123, "ignored") {
		t.Fatal("expected ID allow-list match")
	}
	if !c.allowed(777, "caseuser") {
		t.Fatal("expected username allow-list match")
	}
	if c.allowed(777, "other") {
		t.Fatal("unexpected allow for non-listed sender")
	}

	open := &Channel{}
	if !open.allowed(999, "any") {
		t.Fatal("expected open channel when allow list is empty")
	}
}

func TestChannelToInboundTextAndMetadata(t *testing.T) {
	c := &Channel{}
	update := tgbotapi.Update{
		Message: &tgbotapi.Message{
			MessageID: 44,
			Text:      " hello ",
			From:      &tgbotapi.User{ID: 9, UserName: "alice"},
			Chat:      &tgbotapi.Chat{ID: 111, Type: "group"},
		},
	}

	msg := c.toInbound(update)
	if msg.RequestID != "telegram-111-44" {
		t.Fatalf("unexpected request id: %q", msg.RequestID)
	}
	if msg.SessionID != "telegram:111" {
		t.Fatalf("unexpected session id: %q", msg.SessionID)
	}
	if msg.Content != "hello" {
		t.Fatalf("unexpected content: %q", msg.Content)
	}
	if msg.Metadata["telegram_message_id"] != 44 {
		t.Fatalf("unexpected message id metadata: %+v", msg.Metadata)
	}
	if msg.Metadata["is_group"] != true {
		t.Fatalf("expected group metadata, got %+v", msg.Metadata)
	}
}

func TestChannelToInboundMediaAndFallbacks(t *testing.T) {
	c := &Channel{}

	photoOnly := c.toInbound(tgbotapi.Update{Message: &tgbotapi.Message{
		MessageID: 1,
		From:      &tgbotapi.User{ID: 1},
		Chat:      &tgbotapi.Chat{ID: 2, Type: "private"},
		Photo: []tgbotapi.PhotoSize{
			{FileID: "photo-small"},
			{FileID: "photo-large"},
		},
	}})
	if photoOnly.Content != "[photo]" {
		t.Fatalf("expected photo fallback content, got %q", photoOnly.Content)
	}
	if got := photoOnly.Metadata["photo_file_id"]; got != "photo-large" {
		t.Fatalf("expected largest photo file ID, got %v", got)
	}
	if len(photoOnly.Media) != 1 || photoOnly.Media[0] != "photo-large" {
		t.Fatalf("unexpected photo media: %+v", photoOnly.Media)
	}

	docWithCaption := c.toInbound(tgbotapi.Update{Message: &tgbotapi.Message{
		MessageID: 2,
		Caption:   " caption text ",
		From:      &tgbotapi.User{ID: 1},
		Chat:      &tgbotapi.Chat{ID: 2, Type: "private"},
		Document:  &tgbotapi.Document{FileID: "doc-1", FileName: "note.txt"},
	}})
	if docWithCaption.Content != "caption text" {
		t.Fatalf("expected caption content, got %q", docWithCaption.Content)
	}
	if got := docWithCaption.Metadata["document_file_id"]; got != "doc-1" {
		t.Fatalf("unexpected document metadata: %+v", docWithCaption.Metadata)
	}

	empty := c.toInbound(tgbotapi.Update{Message: &tgbotapi.Message{
		MessageID: 3,
		From:      &tgbotapi.User{ID: 1},
		Chat:      &tgbotapi.Chat{ID: 2, Type: "private"},
	}})
	if empty.Content != "[empty message]" {
		t.Fatalf("expected empty-message fallback, got %q", empty.Content)
	}
}
