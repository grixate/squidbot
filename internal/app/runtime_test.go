package app

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/config"
	storepkg "github.com/grixate/squidbot/internal/storage/bbolt"
	"github.com/grixate/squidbot/internal/telemetry"
)

func TestMetricsHandlerAuthAndLocalhost(t *testing.T) {
	metrics := &telemetry.Metrics{}
	metrics.InboundCount.Add(5)
	h := metricsHandler(metrics, "secret", true)

	forbiddenReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	forbiddenReq.RemoteAddr = "8.8.8.8:12345"
	forbiddenRec := httptest.NewRecorder()
	h(forbiddenRec, forbiddenReq)
	if forbiddenRec.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden status, got %d", forbiddenRec.Code)
	}

	unauthorizedReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	unauthorizedReq.RemoteAddr = "127.0.0.1:12345"
	unauthorizedRec := httptest.NewRecorder()
	h(unauthorizedRec, unauthorizedReq)
	if unauthorizedRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized status, got %d", unauthorizedRec.Code)
	}

	authorizedReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	authorizedReq.RemoteAddr = "127.0.0.1:12345"
	authorizedReq.Header.Set("Authorization", "Bearer secret")
	authorizedRec := httptest.NewRecorder()
	h(authorizedRec, authorizedReq)
	if authorizedRec.Code != http.StatusOK {
		t.Fatalf("expected success status, got %d", authorizedRec.Code)
	}
	if !strings.Contains(authorizedRec.Body.String(), "squidbot_inbound_count 5") {
		t.Fatalf("unexpected metrics body: %s", authorizedRec.Body.String())
	}
}

func TestChannelHelpersAndValidation(t *testing.T) {
	nilMeta := config.GenericChannelConfig{}
	if channelMeta(nilMeta, "listen_addr") != "" {
		t.Fatal("expected empty metadata lookup")
	}

	slackCfg := config.GenericChannelConfig{Token: "token", Metadata: map[string]string{"listen_addr": ":8080"}}
	if !canUseSlackNative(slackCfg) {
		t.Fatal("expected slack native eligibility")
	}
	if err := validateSlackConfig(config.GenericChannelConfig{}); err == nil {
		t.Fatal("expected slack validation error")
	}
	if err := validateSlackConfig(slackCfg); err != nil {
		t.Fatalf("unexpected slack validation error: %v", err)
	}

	discordCfg := config.GenericChannelConfig{Token: "token", Metadata: map[string]string{"listen_addr": ":8080", "public_key": "pub"}}
	if !canUseDiscordNative(discordCfg) {
		t.Fatal("expected discord native eligibility")
	}
	if err := validateDiscordConfig(config.GenericChannelConfig{Token: "token", Metadata: map[string]string{"listen_addr": ":8080"}}); err == nil {
		t.Fatal("expected discord validation error")
	}
	if err := validateDiscordConfig(discordCfg); err != nil {
		t.Fatalf("unexpected discord validation error: %v", err)
	}

	webchatCfg := config.GenericChannelConfig{Metadata: map[string]string{"listen_addr": ":8081"}}
	if !canUseWebChatNative(webchatCfg) {
		t.Fatal("expected webchat native eligibility")
	}
	if err := validateWebChatConfig(config.GenericChannelConfig{}); err == nil {
		t.Fatal("expected webchat validation error")
	}

	whatsCfg := config.GenericChannelConfig{Token: "token", Metadata: map[string]string{"listen_addr": ":8082", "phone_number_id": "123"}}
	if !canUseWhatsAppNative(whatsCfg) {
		t.Fatal("expected whatsapp native eligibility")
	}
	if err := validateWhatsAppConfig(config.GenericChannelConfig{Token: "token", Metadata: map[string]string{"listen_addr": ":8082"}}); err == nil {
		t.Fatal("expected whatsapp validation error")
	}
	if err := validateWhatsAppConfig(whatsCfg); err != nil {
		t.Fatalf("unexpected whatsapp validation error: %v", err)
	}
}

func newRuntimeForHandlerTests(t *testing.T) *Runtime {
	t.Helper()
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = t.TempDir()
	cfg.Storage.DBPath = filepath.Join(t.TempDir(), "runtime-test.db")
	store, err := storepkg.Open(cfg.Storage.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	metrics := &telemetry.Metrics{}
	engine, err := agent.NewEngine(cfg, &federationHTTPProvider{}, "test-model", store, metrics, log.New(io.Discard, "", 0))
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	r := &Runtime{
		Config:  cfg,
		Store:   store,
		Engine:  engine,
		Metrics: metrics,
		log:     log.New(io.Discard, "", 0),
	}
	t.Cleanup(func() {
		_ = engine.Close()
		_ = store.Close()
	})
	return r
}

func TestRuntimeIngressAndAskHandlers(t *testing.T) {
	r := newRuntimeForHandlerTests(t)

	msg := agent.InboundMessage{
		ChatID:   "chat-1",
		SenderID: "user-1",
		Content:  "hello",
	}
	if err := r.telegramIngress()(context.Background(), msg); err != nil {
		t.Fatalf("telegram ingress failed: %v", err)
	}
	if err := r.channelIngress("webchat")(context.Background(), msg); err != nil {
		t.Fatalf("channel ingress failed: %v", err)
	}
	resp, err := r.channelAsk("webchat")(context.Background(), msg)
	if err != nil {
		t.Fatalf("channel ask failed: %v", err)
	}
	if strings.TrimSpace(resp) == "" {
		t.Fatalf("expected non-empty ask response, got %q", resp)
	}

	sink := agent.StreamSinkFunc(func(ctx context.Context, event agent.StreamEvent) error { return nil })
	if err := r.channelAskStream("webchat")(context.Background(), msg, sink); err != nil {
		t.Fatalf("channel ask stream failed: %v", err)
	}
}

func TestTelegramAdapterNilChannelMethods(t *testing.T) {
	adapter := &telegramAdapter{id: "telegram"}
	if adapter.ID() != "telegram" {
		t.Fatalf("unexpected adapter id: %s", adapter.ID())
	}
	if err := adapter.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Send(context.Background(), agent.OutboundMessage{Channel: "telegram", ChatID: "1", Content: "x"}); err != nil {
		t.Fatal(err)
	}
}
