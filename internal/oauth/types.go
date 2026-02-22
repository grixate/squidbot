package oauth

import "time"

const (
	ProviderOpenAICodex = "openai-codex"
)

type Token struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	TokenType    string    `json:"tokenType,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	AccountID    string    `json:"accountId,omitempty"`
	Audience     string    `json:"audience,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
	ObtainedAt   time.Time `json:"obtainedAt,omitempty"`
}

func (t Token) IsZero() bool {
	return t.AccessToken == "" && t.RefreshToken == ""
}

func (t Token) AccessExpired(now time.Time) bool {
	if t.AccessToken == "" {
		return true
	}
	if t.ExpiresAt.IsZero() {
		return false
	}
	return !now.Before(t.ExpiresAt)
}

func (t Token) HasUsableCredentials(now time.Time, refreshSkew time.Duration) bool {
	if t.AccessToken != "" {
		if t.ExpiresAt.IsZero() {
			return true
		}
		if now.Before(t.ExpiresAt.Add(-refreshSkew)) {
			return true
		}
	}
	return t.RefreshToken != ""
}
