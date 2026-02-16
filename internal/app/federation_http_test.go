package app

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/agent"
	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/federation"
	"github.com/grixate/squidbot/internal/provider"
	storepkg "github.com/grixate/squidbot/internal/storage/bbolt"
	"github.com/grixate/squidbot/internal/telemetry"
)

type federationHTTPProvider struct{}

func (p *federationHTTPProvider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{}
}

func (p *federationHTTPProvider) Stream(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamEvent, <-chan error) {
	events := make(chan provider.StreamEvent)
	errs := make(chan error, 1)
	close(events)
	close(errs)
	return events, errs
}

func (p *federationHTTPProvider) Chat(ctx context.Context, req provider.ChatRequest) (provider.ChatResponse, error) {
	return provider.ChatResponse{Content: "ok"}, nil
}

func newFederationHTTPRuntime(t *testing.T) *Runtime {
	t.Helper()
	cfg := config.Default()
	cfg.Agents.Defaults.Workspace = t.TempDir()
	cfg.Storage.DBPath = filepath.Join(t.TempDir(), "federation-http.db")
	cfg.Runtime.Federation.Enabled = true
	cfg.Runtime.Federation.Peers = []config.FederationPeerConfig{
		{ID: "peer-a", BaseURL: "http://peer-a.example", AuthToken: "token-a", Enabled: true},
		{ID: "peer-b", BaseURL: "http://peer-b.example", AuthToken: "token-b", Enabled: true},
	}

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
	t.Cleanup(func() {
		_ = engine.Close()
		_ = store.Close()
	})

	return &Runtime{
		Config:  cfg,
		Store:   store,
		Engine:  engine,
		Metrics: metrics,
		log:     log.New(io.Discard, "", 0),
	}
}

func TestFederationDelegationByIDAuthRequired(t *testing.T) {
	runtime := newFederationHTTPRuntime(t)
	req := httptest.NewRequest(http.MethodGet, "/api/federation/delegations/run-1", nil)
	rec := httptest.NewRecorder()

	runtime.handleFederationDelegationByID(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestFederationDelegationByIDOwnershipEnforced(t *testing.T) {
	runtime := newFederationHTTPRuntime(t)
	now := time.Now().UTC()
	if err := runtime.Store.PutFederationRun(context.Background(), federation.DelegationRun{
		ID:           "run-owned-by-a",
		OriginNodeID: "peer-a",
		Task:         "task",
		Status:       federation.StatusSucceeded,
		CreatedAt:    now,
		FinishedAt:   &now,
		Result:       &federation.DelegationResult{Summary: "ok"},
	}); err != nil {
		t.Fatalf("seed run failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/federation/delegations/run-owned-by-a", nil)
	req.Header.Set("X-Squidbot-Node-ID", "peer-b")
	req.Header.Set("Authorization", "Bearer token-b")
	rec := httptest.NewRecorder()

	runtime.handleFederationDelegationByID(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != federationRunOwnershipError {
		t.Fatalf("unexpected forbidden body: %q", rec.Body.String())
	}
}

func TestFederationDelegationByIDSameOriginStatusResultAndCancel(t *testing.T) {
	runtime := newFederationHTTPRuntime(t)
	now := time.Now().UTC()
	succeeded := federation.DelegationRun{
		ID:           "run-same-origin-ok",
		OriginNodeID: "peer-a",
		Task:         "task",
		Status:       federation.StatusSucceeded,
		CreatedAt:    now,
		FinishedAt:   &now,
		Result:       &federation.DelegationResult{Summary: "done"},
	}
	if err := runtime.Store.PutFederationRun(context.Background(), succeeded); err != nil {
		t.Fatalf("seed succeeded run failed: %v", err)
	}
	if err := runtime.Store.PutFederationRun(context.Background(), federation.DelegationRun{
		ID:           "run-same-origin-cancel",
		OriginNodeID: "peer-a",
		Task:         "cancel me",
		Status:       federation.StatusQueued,
		CreatedAt:    now,
	}); err != nil {
		t.Fatalf("seed cancellable run failed: %v", err)
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/api/federation/delegations/run-same-origin-ok", nil)
	statusReq.Header.Set("X-Squidbot-Node-ID", "peer-a")
	statusReq.Header.Set("Authorization", "Bearer token-a")
	statusRec := httptest.NewRecorder()
	runtime.handleFederationDelegationByID(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("expected status endpoint 200, got %d", statusRec.Code)
	}
	var statusOut federation.DelegationRun
	if err := json.Unmarshal(statusRec.Body.Bytes(), &statusOut); err != nil {
		t.Fatalf("decode status response failed: %v", err)
	}
	if statusOut.ID != succeeded.ID {
		t.Fatalf("unexpected status run id: %s", statusOut.ID)
	}

	resultReq := httptest.NewRequest(http.MethodGet, "/api/federation/delegations/run-same-origin-ok/result", nil)
	resultReq.Header.Set("X-Squidbot-Node-ID", "peer-a")
	resultReq.Header.Set("Authorization", "Bearer token-a")
	resultRec := httptest.NewRecorder()
	runtime.handleFederationDelegationByID(resultRec, resultReq)
	if resultRec.Code != http.StatusOK {
		t.Fatalf("expected result endpoint 200, got %d", resultRec.Code)
	}
	var resultOut federation.DelegationRun
	if err := json.Unmarshal(resultRec.Body.Bytes(), &resultOut); err != nil {
		t.Fatalf("decode result response failed: %v", err)
	}
	if resultOut.Result == nil || resultOut.Result.Summary != "done" {
		t.Fatalf("unexpected result payload: %+v", resultOut.Result)
	}

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/federation/delegations/run-same-origin-cancel/cancel", strings.NewReader(`{}`))
	cancelReq.Header.Set("X-Squidbot-Node-ID", "peer-a")
	cancelReq.Header.Set("Authorization", "Bearer token-a")
	cancelRec := httptest.NewRecorder()
	runtime.handleFederationDelegationByID(cancelRec, cancelReq)
	if cancelRec.Code != http.StatusOK {
		t.Fatalf("expected cancel endpoint 200, got %d", cancelRec.Code)
	}
	var cancelOut federation.DelegationRun
	if err := json.Unmarshal(cancelRec.Body.Bytes(), &cancelOut); err != nil {
		t.Fatalf("decode cancel response failed: %v", err)
	}
	if cancelOut.Status != federation.StatusCancelled {
		t.Fatalf("expected cancelled status, got %s", cancelOut.Status)
	}
}
