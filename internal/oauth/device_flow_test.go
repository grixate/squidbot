package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeviceFlowStartPollAndRefresh(t *testing.T) {
	var devicePollCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.URL.Path {
		case "/device":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":               "dev-1",
				"user_code":                 "CODE-1",
				"verification_uri":          "https://example.com/verify",
				"verification_uri_complete": "https://example.com/verify?code=CODE-1",
				"expires_in":                60,
				"interval":                  1,
			})
		case "/token":
			grant := r.Form.Get("grant_type")
			switch grant {
			case "urn:ietf:params:oauth:grant-type:device_code":
				if devicePollCount.Add(1) == 1 {
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending"})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token":  "access-1",
					"refresh_token": "refresh-1",
					"expires_in":    3600,
					"account_id":    "acct-1",
				})
			case "refresh_token":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token":  "access-2",
					"refresh_token": "refresh-2",
					"expires_in":    3600,
					"account_id":    "acct-1",
				})
			default:
				w.WriteHeader(http.StatusBadRequest)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := &DeviceFlowClient{
		ClientID:      "client-1",
		Audience:      "aud-1",
		DeviceAuthURL: srv.URL + "/device",
		TokenURL:      srv.URL + "/token",
		HTTPClient:    srv.Client(),
		Now:           func() time.Time { return time.Now().UTC() },
	}
	auth, err := client.Start(context.Background())
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if auth.DeviceCode != "dev-1" || auth.UserCode != "CODE-1" {
		t.Fatalf("unexpected auth payload: %#v", auth)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	token, err := client.PollToken(ctx, auth)
	if err != nil {
		t.Fatalf("poll failed: %v", err)
	}
	if token.AccessToken != "access-1" || token.RefreshToken != "refresh-1" {
		t.Fatalf("unexpected token: %#v", token)
	}
	refreshed, err := client.Refresh(context.Background(), token.RefreshToken)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if refreshed.AccessToken != "access-2" {
		t.Fatalf("unexpected refreshed token: %#v", refreshed)
	}
}

func TestParseOAuthHTTPError(t *testing.T) {
	err := parseOAuthHTTPError([]byte(`{"error":"slow_down","error_description":"wait"}`))
	if err.Code != "slow_down" || err.Message != "wait" {
		t.Fatalf("unexpected oauth error parse: %#v", err)
	}
	if !isPendingAuthErr(err) {
		t.Fatal("expected pending auth err")
	}
	if values, _ := url.ParseQuery("a=b"); values.Get("a") != "b" {
		t.Fatal("unexpected parse query behavior")
	}
}
