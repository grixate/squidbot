package oauth

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestTokenStoreSaveLoadDelete(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := NewOpenAICodexTokenStore()
	token := Token{
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
		AccountID:    "acct-1",
	}
	if err := store.Save(token); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected mode: %o", info.Mode().Perm())
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.AccessToken != token.AccessToken || loaded.RefreshToken != token.RefreshToken {
		t.Fatalf("unexpected token payload: %#v", loaded)
	}
	if !store.HasUsableToken(time.Now().UTC(), 2*time.Minute) {
		t.Fatal("expected usable token")
	}
	if err := store.Delete(); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	_, err = store.Load()
	if !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("expected ErrTokenNotFound, got %v", err)
	}
}

func TestTokenStoreRejectsInsecurePermissions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := NewOpenAICodexTokenStore()
	if err := os.MkdirAll(OAuthDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), []byte(`{"accessToken":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("expected insecure permissions error")
	}
}
