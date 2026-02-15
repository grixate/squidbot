package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/config"
)

type Client struct {
	httpClient *http.Client
}

type RequestError struct {
	StatusCode int
	Body       string
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("federation request failed status=%d body=%s", e.StatusCode, strings.TrimSpace(e.Body))
}

func (e *RequestError) Retryable() bool {
	return e != nil && (e.StatusCode == http.StatusRequestTimeout || e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500)
}

func NewClient(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (c *Client) Submit(ctx context.Context, peer config.FederationPeerConfig, req DelegationRequest, originNodeID, idempotencyKey string) (DelegationRun, error) {
	var out DelegationRun
	err := c.doJSON(ctx, peer, http.MethodPost, "/api/federation/delegations", originNodeID, idempotencyKey, req, &out)
	return out, err
}

func (c *Client) Status(ctx context.Context, peer config.FederationPeerConfig, runID, originNodeID string) (DelegationRun, error) {
	path := "/api/federation/delegations/" + url.PathEscape(strings.TrimSpace(runID))
	var out DelegationRun
	err := c.doJSON(ctx, peer, http.MethodGet, path, originNodeID, "", nil, &out)
	return out, err
}

func (c *Client) Result(ctx context.Context, peer config.FederationPeerConfig, runID, originNodeID string) (DelegationRun, error) {
	path := "/api/federation/delegations/" + url.PathEscape(strings.TrimSpace(runID)) + "/result"
	var out DelegationRun
	err := c.doJSON(ctx, peer, http.MethodGet, path, originNodeID, "", nil, &out)
	return out, err
}

func (c *Client) Cancel(ctx context.Context, peer config.FederationPeerConfig, runID, originNodeID string) (DelegationRun, error) {
	path := "/api/federation/delegations/" + url.PathEscape(strings.TrimSpace(runID)) + "/cancel"
	var out DelegationRun
	err := c.doJSON(ctx, peer, http.MethodPost, path, originNodeID, "", map[string]any{}, &out)
	return out, err
}

func (c *Client) Health(ctx context.Context, peer config.FederationPeerConfig, originNodeID string) (PeerHealth, error) {
	var out PeerHealth
	err := c.doJSON(ctx, peer, http.MethodGet, "/api/federation/health", originNodeID, "", nil, &out)
	return out, err
}

func (c *Client) doJSON(ctx context.Context, peer config.FederationPeerConfig, method, path, originNodeID, idempotencyKey string, payload any, out any) error {
	if c == nil {
		return fmt.Errorf("federation client is not configured")
	}
	endpoint := strings.TrimRight(strings.TrimSpace(peer.BaseURL), "/")
	if endpoint == "" {
		return fmt.Errorf("peer %q base URL is empty", strings.TrimSpace(peer.ID))
	}
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(peer.AuthToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if nodeID := strings.TrimSpace(originNodeID); nodeID != "" {
		req.Header.Set("X-Squidbot-Node-ID", nodeID)
	}
	if idem := strings.TrimSpace(idempotencyKey); idem != "" {
		req.Header.Set("X-Idempotency-Key", idem)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return &RequestError{StatusCode: resp.StatusCode, Body: string(responseBody)}
	}
	if len(responseBody) == 0 || out == nil {
		return nil
	}
	if err := json.Unmarshal(responseBody, out); err != nil {
		return err
	}
	return nil
}
