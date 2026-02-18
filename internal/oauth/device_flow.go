package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultCodexDeviceAuthURL = "https://auth.openai.com/oauth/device/code"
	defaultCodexTokenURL      = "https://auth.openai.com/oauth/token"
	defaultCodexClientID      = "codex-cli"
)

type DeviceAuthorization struct {
	DeviceCode              string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	ExpiresIn               time.Duration
	Interval                time.Duration
}

type DeviceFlowClient struct {
	ClientID      string
	Audience      string
	DeviceAuthURL string
	TokenURL      string
	HTTPClient    *http.Client
	Now           func() time.Time
}

func NewOpenAICodexDeviceFlowClientFromEnv() *DeviceFlowClient {
	clientID := strings.TrimSpace(os.Getenv("SQUIDBOT_OAUTH_OPENAI_CODEX_CLIENT_ID"))
	if clientID == "" {
		clientID = defaultCodexClientID
	}
	audience := strings.TrimSpace(os.Getenv("SQUIDBOT_OAUTH_OPENAI_CODEX_AUDIENCE"))
	deviceURL := strings.TrimSpace(os.Getenv("SQUIDBOT_OAUTH_OPENAI_CODEX_DEVICE_AUTH_URL"))
	if deviceURL == "" {
		deviceURL = defaultCodexDeviceAuthURL
	}
	tokenURL := strings.TrimSpace(os.Getenv("SQUIDBOT_OAUTH_OPENAI_CODEX_TOKEN_URL"))
	if tokenURL == "" {
		tokenURL = defaultCodexTokenURL
	}
	return &DeviceFlowClient{
		ClientID:      clientID,
		Audience:      audience,
		DeviceAuthURL: deviceURL,
		TokenURL:      tokenURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		Now: func() time.Time { return time.Now().UTC() },
	}
}

func (c *DeviceFlowClient) Start(ctx context.Context) (DeviceAuthorization, error) {
	if c == nil {
		return DeviceAuthorization{}, fmt.Errorf("device flow client is nil")
	}
	values := url.Values{}
	values.Set("client_id", strings.TrimSpace(c.ClientID))
	if audience := strings.TrimSpace(c.Audience); audience != "" {
		values.Set("audience", audience)
	}
	body, err := c.postForm(ctx, c.DeviceAuthURL, values)
	if err != nil {
		return DeviceAuthorization{}, err
	}
	var raw struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return DeviceAuthorization{}, err
	}
	if strings.TrimSpace(raw.DeviceCode) == "" || strings.TrimSpace(raw.UserCode) == "" || strings.TrimSpace(raw.VerificationURI) == "" {
		return DeviceAuthorization{}, fmt.Errorf("oauth device authorization response missing required fields")
	}
	interval := raw.Interval
	if interval <= 0 {
		interval = 5
	}
	expires := raw.ExpiresIn
	if expires <= 0 {
		expires = 600
	}
	return DeviceAuthorization{
		DeviceCode:              raw.DeviceCode,
		UserCode:                raw.UserCode,
		VerificationURI:         raw.VerificationURI,
		VerificationURIComplete: raw.VerificationURIComplete,
		ExpiresIn:               time.Duration(expires) * time.Second,
		Interval:                time.Duration(interval) * time.Second,
	}, nil
}

func (c *DeviceFlowClient) PollToken(ctx context.Context, auth DeviceAuthorization) (Token, error) {
	if c == nil {
		return Token{}, fmt.Errorf("device flow client is nil")
	}
	if strings.TrimSpace(auth.DeviceCode) == "" {
		return Token{}, fmt.Errorf("device code is required")
	}
	nowFn := c.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	interval := auth.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := nowFn().Add(auth.ExpiresIn)
	if auth.ExpiresIn <= 0 {
		deadline = nowFn().Add(10 * time.Minute)
	}

	for {
		if err := ctx.Err(); err != nil {
			return Token{}, err
		}
		if nowFn().After(deadline) {
			return Token{}, fmt.Errorf("oauth device authorization expired")
		}
		token, err, slowDown := c.requestToken(ctx, url.Values{
			"grant_type":  []string{"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   []string{strings.TrimSpace(c.ClientID)},
			"device_code": []string{auth.DeviceCode},
		})
		if err == nil {
			return token, nil
		}
		if slowDown {
			interval += 2 * time.Second
		}
		if !isPendingAuthErr(err) {
			return Token{}, err
		}
		select {
		case <-ctx.Done():
			return Token{}, ctx.Err()
		case <-time.After(interval):
		}
	}
}

func (c *DeviceFlowClient) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	if c == nil {
		return Token{}, fmt.Errorf("device flow client is nil")
	}
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return Token{}, fmt.Errorf("refresh token is required")
	}
	token, err, _ := c.requestToken(ctx, url.Values{
		"grant_type":    []string{"refresh_token"},
		"client_id":     []string{strings.TrimSpace(c.ClientID)},
		"refresh_token": []string{refreshToken},
	})
	return token, err
}

func (c *DeviceFlowClient) requestToken(ctx context.Context, values url.Values) (Token, error, bool) {
	body, err := c.postForm(ctx, c.TokenURL, values)
	if err != nil {
		var oauthErr *oauthHTTPError
		if ok := asOAuthError(err, &oauthErr); ok {
			if oauthErr.Code == "slow_down" {
				return Token{}, oauthErr, true
			}
		}
		return Token{}, err, false
	}
	nowFn := c.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
		AccountID    string `json:"account_id"`
		Audience     string `json:"audience"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Token{}, err, false
	}
	if strings.TrimSpace(raw.AccessToken) == "" {
		return Token{}, fmt.Errorf("oauth token response missing access_token"), false
	}
	now := nowFn()
	expiresAt := time.Time{}
	if raw.ExpiresIn > 0 {
		expiresAt = now.Add(time.Duration(raw.ExpiresIn) * time.Second)
	}
	return Token{
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		TokenType:    raw.TokenType,
		Scope:        raw.Scope,
		AccountID:    raw.AccountID,
		Audience:     raw.Audience,
		ObtainedAt:   now,
		ExpiresAt:    expiresAt,
	}, nil, false
}

func (c *DeviceFlowClient) postForm(ctx context.Context, endpoint string, values url.Values) ([]byte, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("oauth endpoint is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode >= 300 {
		oauthErr := parseOAuthHTTPError(body)
		oauthErr.StatusCode = resp.StatusCode
		if oauthErr.Message == "" {
			oauthErr.Message = resp.Status
		}
		return nil, oauthErr
	}
	return body, nil
}

type oauthHTTPError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *oauthHTTPError) Error() string {
	parts := make([]string, 0, 3)
	if e.StatusCode > 0 {
		parts = append(parts, strconv.Itoa(e.StatusCode))
	}
	if e.Code != "" {
		parts = append(parts, e.Code)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	if len(parts) == 0 {
		return "oauth error"
	}
	return "oauth error: " + strings.Join(parts, " ")
}

func parseOAuthHTTPError(body []byte) *oauthHTTPError {
	var parsed struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		Message          string `json:"message"`
	}
	_ = json.Unmarshal(body, &parsed)
	message := strings.TrimSpace(parsed.ErrorDescription)
	if message == "" {
		message = strings.TrimSpace(parsed.Message)
	}
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	return &oauthHTTPError{
		Code:    strings.TrimSpace(parsed.Error),
		Message: message,
	}
}

func asOAuthError(err error, out **oauthHTTPError) bool {
	if err == nil {
		return false
	}
	typed, ok := err.(*oauthHTTPError)
	if !ok {
		return false
	}
	if out != nil {
		*out = typed
	}
	return true
}

func isPendingAuthErr(err error) bool {
	typed, ok := err.(*oauthHTTPError)
	if !ok {
		return false
	}
	switch typed.Code {
	case "authorization_pending", "slow_down":
		return true
	default:
		return false
	}
}
