package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrTokenNotFound = errors.New("oauth token not found")

type TokenStore struct {
	provider string
	path     string
}

func NewTokenStore(provider string) *TokenStore {
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" {
		provider = ProviderOpenAICodex
	}
	return &TokenStore{
		provider: provider,
		path:     filepath.Join(OAuthDir(), provider+".json"),
	}
}

func NewOpenAICodexTokenStore() *TokenStore {
	return NewTokenStore(ProviderOpenAICodex)
}

func OAuthDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".squidbot", "oauth")
	}
	return filepath.Join(home, ".squidbot", "oauth")
}

func (s *TokenStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *TokenStore) Save(token Token) error {
	if s == nil {
		return fmt.Errorf("token store is nil")
	}
	if strings.TrimSpace(token.AccessToken) == "" && strings.TrimSpace(token.RefreshToken) == "" {
		return fmt.Errorf("cannot save empty oauth token")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	if token.ObtainedAt.IsZero() {
		token.ObtainedAt = time.Now().UTC()
	}
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(s.path, 0o600)
}

func (s *TokenStore) Load() (Token, error) {
	if s == nil {
		return Token{}, fmt.Errorf("token store is nil")
	}
	info, err := os.Stat(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return Token{}, ErrTokenNotFound
		}
		return Token{}, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return Token{}, fmt.Errorf("insecure oauth token permissions on %s", s.path)
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return Token{}, err
	}
	var token Token
	if err := json.Unmarshal(data, &token); err != nil {
		return Token{}, err
	}
	if token.IsZero() {
		return Token{}, fmt.Errorf("oauth token file is empty")
	}
	return token, nil
}

func (s *TokenStore) Delete() error {
	if s == nil {
		return nil
	}
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *TokenStore) HasUsableToken(now time.Time, refreshSkew time.Duration) bool {
	token, err := s.Load()
	if err != nil {
		return false
	}
	return token.HasUsableCredentials(now, refreshSkew)
}
