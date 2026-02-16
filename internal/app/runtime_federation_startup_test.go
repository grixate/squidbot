package app

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/config"
)

func TestStartGatewayFailsFastWhenFederationListenerFails(t *testing.T) {
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = t.TempDir()
	cfg.Storage.DBPath = filepath.Join(t.TempDir(), "gateway-federation.db")
	cfg.Providers.Active = config.ProviderOllama
	_ = cfg.SetProviderByName(config.ProviderOllama, config.ProviderConfig{
		APIBase: "http://127.0.0.1:11434/v1",
		Model:   "llama3.1:8b",
	})
	cfg.Runtime.Federation.Enabled = true
	cfg.Runtime.Federation.ListenAddr = "127.0.0.1:18900"

	runtime, err := BuildRuntime(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("build runtime failed: %v", err)
	}
	t.Cleanup(func() {
		_ = runtime.Shutdown()
	})

	runtime.federationListen = func(network, address string) (net.Listener, error) {
		return nil, errors.New("synthetic federation bind failure")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = runtime.StartGateway(ctx)
	if err == nil {
		t.Fatal("expected StartGateway to fail when federation listener fails")
	}
	if !strings.Contains(err.Error(), "synthetic federation bind failure") {
		t.Fatalf("expected bind failure in error, got: %v", err)
	}
}
