package management

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/config"
)

func postJSON(t *testing.T, client *http.Client, url string, payload any, cookies ...*http.Cookie) (*http.Response, []byte) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
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

func get(t *testing.T, client *http.Client, url string, cookies ...*http.Cookie) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
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

func TestSetupProviderTestRequiresToken(t *testing.T) {
	cfg := config.Default()
	cfg.Management.Port = 0
	cfg.Storage.DBPath = filepath.Join(t.TempDir(), "squidbot.db")
	server, err := NewServer(cfg, Options{RequireSetupToken: true})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	ts := httptest.NewServer(server.routes())
	defer ts.Close()

	resp, _ := postJSON(t, ts.Client(), ts.URL+"/api/setup/provider/test", map[string]any{
		"provider": "ollama",
		"model":    "llama3.1:8b",
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %d", resp.StatusCode)
	}
}

func TestSetupCompletePersistsConfigAndHash(t *testing.T) {
	cfg := config.Default()
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg.Management.Port = 0
	cfg.Storage.DBPath = filepath.Join(t.TempDir(), "squidbot.db")
	server, err := NewServer(cfg, Options{
		ConfigPath:        configPath,
		RequireSetupToken: true,
	})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	ts := httptest.NewServer(server.routes())
	defer ts.Close()

	resp, body := postJSON(t, ts.Client(), ts.URL+"/api/setup/complete", map[string]any{
		"setupToken": server.SetupToken(),
		"provider":   "ollama",
		"model":      "llama3.1:8b",
		"channel": map[string]any{
			"id":      "telegram",
			"enabled": false,
		},
		"password": "very-secure-password",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", resp.StatusCode, string(body))
	}
	select {
	case <-server.SetupCompleted():
	case <-time.After(time.Second):
		t.Fatal("expected setup completion signal")
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if loaded.Auth.PasswordHash == "" {
		t.Fatal("expected password hash to be persisted")
	}
	if !VerifyPassword("very-secure-password", loaded.Auth.PasswordHash) {
		t.Fatal("expected stored password hash to verify")
	}
}

func TestLoginSessionAndLogoutFlow(t *testing.T) {
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
	cfg.Management.Port = 0

	server, err := NewServer(cfg, Options{RequireSetupToken: true})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	ts := httptest.NewServer(server.routes())
	defer ts.Close()

	resp, _ := get(t, ts.Client(), ts.URL+"/api/manage/placeholder")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

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
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie")
	}

	protectedResp, _ := get(t, ts.Client(), ts.URL+"/api/manage/placeholder", sessionCookie)
	if protectedResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after login, got %d", protectedResp.StatusCode)
	}

	logoutResp, _ := postJSON(t, ts.Client(), ts.URL+"/api/auth/logout", map[string]any{}, sessionCookie)
	if logoutResp.StatusCode != http.StatusOK {
		t.Fatalf("expected logout success, got %d", logoutResp.StatusCode)
	}
	protectedResp, _ = get(t, ts.Client(), ts.URL+"/api/manage/placeholder", sessionCookie)
	if protectedResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 after logout, got %d", protectedResp.StatusCode)
	}
}
