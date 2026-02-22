package oauth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrLoginRequired = errors.New("oauth login required")

type TokenManager struct {
	store       *TokenStore
	client      *DeviceFlowClient
	refreshSkew time.Duration
	mu          sync.Mutex
}

func NewTokenManager(store *TokenStore, client *DeviceFlowClient) *TokenManager {
	if store == nil {
		store = NewOpenAICodexTokenStore()
	}
	if client == nil {
		client = NewOpenAICodexDeviceFlowClientFromEnv()
	}
	return &TokenManager{
		store:       store,
		client:      client,
		refreshSkew: 2 * time.Minute,
	}
}

func NewOpenAICodexTokenManager() *TokenManager {
	return NewTokenManager(NewOpenAICodexTokenStore(), NewOpenAICodexDeviceFlowClientFromEnv())
}

func (m *TokenManager) HasUsableToken() bool {
	if m == nil || m.store == nil {
		return false
	}
	return m.store.HasUsableToken(time.Now().UTC(), m.refreshSkew)
}

func (m *TokenManager) GetValidToken(ctx context.Context) (Token, error) {
	if m == nil {
		return Token{}, fmt.Errorf("token manager is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	token, err := m.store.Load()
	if err != nil {
		if errors.Is(err, ErrTokenNotFound) {
			return Token{}, fmt.Errorf("%w: run `squidbot provider login openai-codex`", ErrLoginRequired)
		}
		return Token{}, err
	}
	if token.HasUsableCredentials(now, m.refreshSkew) && !token.AccessExpired(now.Add(m.refreshSkew)) {
		return token, nil
	}
	if token.RefreshToken == "" {
		return Token{}, fmt.Errorf("%w: run `squidbot provider login openai-codex`", ErrLoginRequired)
	}

	refreshed, refreshErr := m.client.Refresh(ctx, token.RefreshToken)
	if refreshErr != nil {
		return Token{}, fmt.Errorf("refresh oauth token: %w", refreshErr)
	}
	if refreshed.AccountID == "" {
		refreshed.AccountID = token.AccountID
	}
	if refreshed.Audience == "" {
		refreshed.Audience = token.Audience
	}
	if err := m.store.Save(refreshed); err != nil {
		return Token{}, err
	}
	return refreshed, nil
}
