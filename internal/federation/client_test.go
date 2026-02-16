package federation

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/grixate/squidbot/internal/config"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestHealthUsesDefaultEndpointWhenNotConfigured(t *testing.T) {
	var gotPath string
	client := &Client{
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				gotPath = req.URL.Path
				return jsonResponse(http.StatusOK, `{"peer_id":"peer-a","available":true}`), nil
			}),
		},
	}
	_, err := client.Health(context.Background(), config.FederationPeerConfig{
		ID:        "peer-a",
		BaseURL:   "https://peer-a.example",
		AuthToken: "tok-a",
		Enabled:   true,
	}, "origin-node")
	if err != nil {
		t.Fatalf("health call failed: %v", err)
	}
	if gotPath != "/api/federation/health" {
		t.Fatalf("expected default health path, got %q", gotPath)
	}
}

func TestHealthUsesConfiguredEndpointWhenProvided(t *testing.T) {
	var gotPath string
	client := &Client{
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				gotPath = req.URL.Path
				return jsonResponse(http.StatusOK, `{"peer_id":"peer-b","available":true}`), nil
			}),
		},
	}
	_, err := client.Health(context.Background(), config.FederationPeerConfig{
		ID:             "peer-b",
		BaseURL:        "https://peer-b.example/",
		AuthToken:      "tok-b",
		Enabled:        true,
		HealthEndpoint: " custom/health ",
	}, "origin-node")
	if err != nil {
		t.Fatalf("health call failed: %v", err)
	}
	if gotPath != "/custom/health" {
		t.Fatalf("expected normalized custom path, got %q", gotPath)
	}
}
