package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTokenManagerRefreshesExpiredToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := NewOpenAICodexTokenStore()
	if err := store.Save(Token{
		AccessToken:  "expired",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().UTC().Add(-time.Minute),
		AccountID:    "acct-1",
	}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.URL.Path != "/token" || r.Form.Get("grant_type") != "refresh_token" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-new",
			"refresh_token": "refresh-new",
			"expires_in":    3600,
			"account_id":    "acct-1",
		})
	}))
	defer srv.Close()

	client := &DeviceFlowClient{
		ClientID:   "client-1",
		TokenURL:   srv.URL + "/token",
		HTTPClient: srv.Client(),
		Now:        func() time.Time { return time.Now().UTC() },
	}
	manager := NewTokenManager(store, client)
	token, err := manager.GetValidToken(context.Background())
	if err != nil {
		t.Fatalf("get valid token failed: %v", err)
	}
	if token.AccessToken != "access-new" {
		t.Fatalf("unexpected refreshed token: %#v", token)
	}
	if !manager.HasUsableToken() {
		t.Fatal("expected usable token after refresh")
	}
}
