package management

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/federation"
)

func managedServerForTests(t *testing.T) (*Server, *httptest.Server, *http.Cookie) {
	t.Helper()
	cfg := config.Default()
	cfg.Providers.Active = config.ProviderOllama
	cfg.Providers.Ollama.Model = "llama3.1:8b"
	cfg.Storage.DBPath = filepath.Join(t.TempDir(), "squidbot.db")
	hash, err := HashPassword("very-secure-password")
	if err != nil {
		t.Fatalf("hash failed: %v", err)
	}
	cfg.Auth.PasswordHash = hash
	cfg.Auth.PasswordUpdatedAt = time.Now().UTC().Format(time.RFC3339)

	server, err := NewServer(cfg, Options{RequireSetupToken: true})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	ts := httptest.NewServer(server.routes())
	t.Cleanup(ts.Close)

	loginResp, body := postJSON(t, ts.Client(), ts.URL+"/api/auth/login", map[string]any{
		"password": "very-secure-password",
	})
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("expected login success, got %d (%s)", loginResp.StatusCode, string(body))
	}
	var sessionCookie *http.Cookie
	for _, cookie := range loginResp.Cookies() {
		if cookie.Name == sessionCookieName {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie")
	}
	return server, ts, sessionCookie
}

func TestManageOverviewRequiresAuth(t *testing.T) {
	_, ts, sessionCookie := managedServerForTests(t)

	unauth, _ := get(t, ts.Client(), ts.URL+"/api/manage/overview")
	if unauth.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", unauth.StatusCode)
	}
	auth, _ := get(t, ts.Client(), ts.URL+"/api/manage/overview", sessionCookie)
	if auth.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", auth.StatusCode)
	}
}

func TestManageKanbanCRUD(t *testing.T) {
	_, ts, sessionCookie := managedServerForTests(t)

	createResp, body := postJSON(t, ts.Client(), ts.URL+"/api/manage/kanban/tasks", map[string]any{
		"title":    "Test mission task",
		"columnId": "backlog",
		"source": map[string]any{
			"type": "manual",
		},
		"dedupe": false,
	}, sessionCookie)
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("create task failed: %d (%s)", createResp.StatusCode, string(body))
	}
	var created struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal create response failed: %v", err)
	}
	if created.Task.ID == "" {
		t.Fatal("expected task id")
	}

	boardResp, boardBody := get(t, ts.Client(), ts.URL+"/api/manage/kanban", sessionCookie)
	if boardResp.StatusCode != http.StatusOK {
		t.Fatalf("kanban fetch failed: %d (%s)", boardResp.StatusCode, string(boardBody))
	}

	patchResp, patchBody := postJSON(t, ts.Client(), ts.URL+"/api/manage/kanban/tasks/"+created.Task.ID+"/move", map[string]any{
		"columnId": "in_progress",
		"position": 0,
	}, sessionCookie)
	if patchResp.StatusCode != http.StatusOK {
		t.Fatalf("move failed: %d (%s)", patchResp.StatusCode, string(patchBody))
	}

	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/manage/kanban/tasks/"+created.Task.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(sessionCookie)
	delResp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("delete failed: %d", delResp.StatusCode)
	}
	_ = delResp.Body.Close()
}

func requestJSON(t *testing.T, client *http.Client, method, url string, payload any, cookies ...*http.Cookie) (*http.Response, []byte) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, data
}

func TestManageFederationSettingsAndPeerCRUD(t *testing.T) {
	_, ts, sessionCookie := managedServerForTests(t)

	unauth, _ := get(t, ts.Client(), ts.URL+"/api/manage/federation/settings")
	if unauth.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauth settings, got %d", unauth.StatusCode)
	}

	updateResp, updateBody := requestJSON(t, ts.Client(), http.MethodPut, ts.URL+"/api/manage/federation/settings", map[string]any{
		"enabled":           true,
		"nodeId":            "node-alpha",
		"listenAddr":        "127.0.0.1:19010",
		"requestTimeoutSec": 21,
		"maxRetries":        2,
		"retryBackoffMs":    450,
		"autoFallback":      true,
		"allowFromNodeIDs":  []string{"peer-a", "peer-b"},
	}, sessionCookie)
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("expected settings update 200, got %d (%s)", updateResp.StatusCode, string(updateBody))
	}

	peerMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"peer_id":"peer-a","available":true,"queue_depth":1,"max_queue":10,"active_runs":1}`))
	}))
	t.Cleanup(peerMock.Close)

	createResp, createBody := postJSON(t, ts.Client(), ts.URL+"/api/manage/federation/peers", map[string]any{
		"id":            "peer-a",
		"baseUrl":       peerMock.URL,
		"authToken":     "secret-token",
		"enabled":       true,
		"capabilities":  []string{"analysis", "build"},
		"roles":         []string{"planner"},
		"priority":      5,
		"maxConcurrent": 3,
		"maxQueue":      15,
	}, sessionCookie)
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("expected peer create 200, got %d (%s)", createResp.StatusCode, string(createBody))
	}

	peersResp, peersBody := get(t, ts.Client(), ts.URL+"/api/manage/federation/peers", sessionCookie)
	if peersResp.StatusCode != http.StatusOK {
		t.Fatalf("expected peers get 200, got %d (%s)", peersResp.StatusCode, string(peersBody))
	}
	var peersPayload struct {
		Peers []struct {
			ID           string `json:"id"`
			AuthTokenSet bool   `json:"authTokenSet"`
		} `json:"peers"`
	}
	if err := json.Unmarshal(peersBody, &peersPayload); err != nil {
		t.Fatalf("failed to decode peers payload: %v", err)
	}
	if len(peersPayload.Peers) != 1 {
		t.Fatalf("expected one peer, got %d", len(peersPayload.Peers))
	}
	if !peersPayload.Peers[0].AuthTokenSet {
		t.Fatal("expected authTokenSet=true for created peer")
	}

	testResp, testBody := postJSON(t, ts.Client(), ts.URL+"/api/manage/federation/peers/peer-a/test", map[string]any{
		"originNodeId": "mission-control-test",
	}, sessionCookie)
	if testResp.StatusCode != http.StatusOK {
		t.Fatalf("expected peer test 200, got %d (%s)", testResp.StatusCode, string(testBody))
	}

	patchResp, patchBody := requestJSON(t, ts.Client(), http.MethodPatch, ts.URL+"/api/manage/federation/peers/peer-a", map[string]any{
		"enabled":  false,
		"priority": 9,
	}, sessionCookie)
	if patchResp.StatusCode != http.StatusOK {
		t.Fatalf("expected patch 200, got %d (%s)", patchResp.StatusCode, string(patchBody))
	}

	deleteResp, deleteBody := requestJSON(t, ts.Client(), http.MethodDelete, ts.URL+"/api/manage/federation/peers/peer-a", nil, sessionCookie)
	if deleteResp.StatusCode != http.StatusOK {
		t.Fatalf("expected delete 200, got %d (%s)", deleteResp.StatusCode, string(deleteBody))
	}
}

func TestManageFederationRunsEndpoints(t *testing.T) {
	server, ts, sessionCookie := managedServerForTests(t)
	now := time.Now().UTC()
	run := federation.DelegationRun{
		ID:           "fed-run-1",
		OriginNodeID: "node-alpha",
		SessionID:    "session-1",
		Task:         "summarize logs",
		Status:       federation.StatusSucceeded,
		CreatedAt:    now,
		FinishedAt:   &now,
		PeerID:       "peer-a",
		Context: federation.ContextPacket{
			Mode:      "minimal",
			CreatedAt: now,
		},
		Result: &federation.DelegationResult{
			Summary: "ok",
			Output:  "done",
		},
	}
	if err := server.mission.store.PutFederationRun(context.Background(), run); err != nil {
		t.Fatalf("seed federation run failed: %v", err)
	}

	listResp, listBody := get(t, ts.Client(), ts.URL+"/api/manage/federation/runs?session=session-1&limit=10", sessionCookie)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected runs list 200, got %d (%s)", listResp.StatusCode, string(listBody))
	}
	var listPayload struct {
		Runs []federation.DelegationRun `json:"runs"`
	}
	if err := json.Unmarshal(listBody, &listPayload); err != nil {
		t.Fatalf("decode runs list failed: %v", err)
	}
	if len(listPayload.Runs) != 1 || listPayload.Runs[0].ID != run.ID {
		t.Fatalf("unexpected runs list payload: %+v", listPayload.Runs)
	}

	detailResp, detailBody := get(t, ts.Client(), ts.URL+"/api/manage/federation/runs/"+run.ID, sessionCookie)
	if detailResp.StatusCode != http.StatusOK {
		t.Fatalf("expected run detail 200, got %d (%s)", detailResp.StatusCode, string(detailBody))
	}
	var detail federation.DelegationRun
	if err := json.Unmarshal(detailBody, &detail); err != nil {
		t.Fatalf("decode run detail failed: %v", err)
	}
	if detail.ID != run.ID {
		t.Fatalf("expected run id %s, got %s", run.ID, detail.ID)
	}
}
