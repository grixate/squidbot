package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/config"
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
