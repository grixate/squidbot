package management

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grixate/squidbot/internal/config"
	"github.com/grixate/squidbot/internal/setupauth"
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
		"provider": map[string]any{
			"id":    "ollama",
			"model": "llama3.1:8b",
		},
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %d", resp.StatusCode)
	}
}

func TestSetupPasswordSuggestRequiresTokenWhenEnabled(t *testing.T) {
	cfg := config.Default()
	cfg.Management.Port = 0
	cfg.Storage.DBPath = filepath.Join(t.TempDir(), "squidbot.db")
	server, err := NewServer(cfg, Options{RequireSetupToken: true})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	ts := httptest.NewServer(server.routes())
	defer ts.Close()

	resp, _ := postJSON(t, ts.Client(), ts.URL+"/api/setup/password/suggest", map[string]any{})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %d", resp.StatusCode)
	}

	resp, body := postJSON(t, ts.Client(), ts.URL+"/api/setup/password/suggest", map[string]any{
		"setupToken": server.SetupToken(),
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", resp.StatusCode, string(body))
	}
	var payload struct {
		Password string `json:"password"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(payload.Password) != 20 {
		t.Fatalf("expected 20-char password, got %q", payload.Password)
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
		Host:              "0.0.0.0",
		Port:              18791,
		PublicBaseURL:     "http://example.com:18791",
	})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	ts := httptest.NewServer(server.routes())
	defer ts.Close()

	resp, body := postJSON(t, ts.Client(), ts.URL+"/api/setup/complete", map[string]any{
		"setupToken":       server.SetupToken(),
		"activeProviderId": "ollama",
		"providers": []map[string]any{
			{"id": "ollama", "model": "llama3.1:8b"},
			{"id": "openai", "apiKey": "test-key"},
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
	if !setupauth.VerifyPassword("very-secure-password", loaded.Auth.PasswordHash) {
		t.Fatal("expected stored password hash to verify")
	}
	if loaded.Providers.Active != "ollama" {
		t.Fatalf("expected active provider to persist, got %q", loaded.Providers.Active)
	}
	if _, ok := loaded.ProviderByName("openai"); !ok {
		t.Fatal("expected secondary provider to persist")
	}
	if loaded.Management.Host != "0.0.0.0" {
		t.Fatalf("expected management host override to persist, got %q", loaded.Management.Host)
	}
	if loaded.Management.Port != 18791 {
		t.Fatalf("expected management port override to persist, got %d", loaded.Management.Port)
	}
	if loaded.Management.PublicBaseURL != "http://example.com:18791" {
		t.Fatalf("expected management public URL override to persist, got %q", loaded.Management.PublicBaseURL)
	}
}

func TestSetupCompleteRejectsDuplicateProviders(t *testing.T) {
	cfg := config.Default()
	cfg.Management.Port = 0
	cfg.Storage.DBPath = filepath.Join(t.TempDir(), "squidbot.db")
	server, err := NewServer(cfg, Options{RequireSetupToken: true})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	ts := httptest.NewServer(server.routes())
	defer ts.Close()

	resp, _ := postJSON(t, ts.Client(), ts.URL+"/api/setup/complete", map[string]any{
		"setupToken":       server.SetupToken(),
		"activeProviderId": "openai",
		"providers": []map[string]any{
			{"id": "openai", "apiKey": "test-key"},
			{"id": "openai", "apiKey": "other"},
		},
		"password": "very-secure-password",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestLoginSessionAndProtectedAppFlow(t *testing.T) {
	cfg := config.Default()
	cfg.Providers.Active = config.ProviderOllama
	cfg.Providers.Ollama.Model = "llama3.1:8b"
	cfg.Management.Port = 0
	cfg.Storage.DBPath = filepath.Join(t.TempDir(), "squidbot.db")
	hash, err := setupauth.HashPassword("very-secure-password")
	if err != nil {
		t.Fatalf("hash failed: %v", err)
	}
	cfg.Auth.PasswordHash = hash
	cfg.Auth.PasswordUpdatedAt = time.Now().UTC().Format(time.RFC3339)

	server, err := NewServer(cfg, Options{})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	ts := httptest.NewServer(server.routes())
	defer ts.Close()

	noRedirectClient := &http.Client{
		Transport: ts.Client().Transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, _ := get(t, noRedirectClient, ts.URL+"/app")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect when unauthenticated, got %d", resp.StatusCode)
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

	protectedResp, _ := get(t, noRedirectClient, ts.URL+"/app", sessionCookie)
	if protectedResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after login, got %d", protectedResp.StatusCode)
	}

	logoutResp, _ := postJSON(t, ts.Client(), ts.URL+"/api/auth/logout", map[string]any{}, sessionCookie)
	if logoutResp.StatusCode != http.StatusOK {
		t.Fatalf("expected logout success, got %d", logoutResp.StatusCode)
	}

	protectedResp, _ = get(t, noRedirectClient, ts.URL+"/app", sessionCookie)
	if protectedResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect after logout, got %d", protectedResp.StatusCode)
	}
}

func TestDisplayURLsShowsLocalBindAndRemoteHint(t *testing.T) {
	cfg := config.Default()
	cfg.Management.Host = "0.0.0.0"
	cfg.Management.Port = 18790

	server, err := NewServer(cfg, Options{})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}

	urls := server.DisplayURLs()
	if urls.LocalURL != "http://127.0.0.1:18790" {
		t.Fatalf("unexpected local URL: %q", urls.LocalURL)
	}
	if urls.BindURL != "http://0.0.0.0:18790" {
		t.Fatalf("unexpected bind URL: %q", urls.BindURL)
	}
	if !strings.Contains(urls.RemoteURLHint, "<public-host-or-ip>:18790") {
		t.Fatalf("expected remote hint, got %q", urls.RemoteURLHint)
	}
}

func TestDisplayURLsUsesConfiguredPublicURL(t *testing.T) {
	cfg := config.Default()
	cfg.Management.Host = "0.0.0.0"
	cfg.Management.Port = 18790

	server, err := NewServer(cfg, Options{PublicBaseURL: "https://bot.example.com"})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}

	urls := server.DisplayURLs()
	if urls.RemoteURL != "https://bot.example.com" {
		t.Fatalf("unexpected remote URL: %q", urls.RemoteURL)
	}
	if urls.RemoteURLHint != "" {
		t.Fatalf("expected empty remote hint, got %q", urls.RemoteURLHint)
	}
}
