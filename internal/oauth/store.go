package oauth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/grixate/squidbot/internal/secrets"
)

var ErrTokenNotFound = errors.New("oauth token not found")

type TokenStore struct {
	provider string
	path     string
}

type encryptedTokenBlob struct {
	Encrypted  bool   `json:"encrypted"`
	Algorithm  string `json:"algorithm,omitempty"`
	Nonce      string `json:"nonce,omitempty"`
	Ciphertext string `json:"ciphertext,omitempty"`
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
	key, err := oauthStoreKey()
	if err != nil {
		return err
	}
	if len(key) > 0 {
		data, err = encryptTokenPayload(data, key)
		if err != nil {
			return err
		}
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
	key, err := oauthStoreKey()
	if err != nil {
		return Token{}, err
	}
	data, err = maybeDecryptTokenPayload(data, key)
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

func oauthStoreKey() ([]byte, error) {
	ref := strings.TrimSpace(os.Getenv("SQUIDBOT_OAUTH_STORE_KEY_REF"))
	keyMaterial := ""
	if ref != "" {
		resolver := secrets.NewResolver()
		value, err := resolver.Resolve(ref)
		if err != nil {
			return nil, fmt.Errorf("resolve oauth store key ref: %w", err)
		}
		keyMaterial = value
	} else {
		keyMaterial = strings.TrimSpace(os.Getenv("SQUIDBOT_OAUTH_STORE_KEY"))
	}
	if keyMaterial == "" {
		return nil, nil
	}
	sum := sha256.Sum256([]byte(keyMaterial))
	return sum[:], nil
}

func encryptTokenPayload(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, data, nil)
	blob := encryptedTokenBlob{
		Encrypted:  true,
		Algorithm:  "aes-256-gcm",
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}
	return json.MarshalIndent(blob, "", "  ")
}

func maybeDecryptTokenPayload(data, key []byte) ([]byte, error) {
	var blob encryptedTokenBlob
	if err := json.Unmarshal(data, &blob); err != nil || !blob.Encrypted {
		return data, nil
	}
	if len(key) == 0 {
		return nil, fmt.Errorf("oauth token is encrypted but SQUIDBOT_OAUTH_STORE_KEY_REF or SQUIDBOT_OAUTH_STORE_KEY is not configured")
	}
	nonce, err := base64.StdEncoding.DecodeString(strings.TrimSpace(blob.Nonce))
	if err != nil {
		return nil, err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(blob.Ciphertext))
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}
