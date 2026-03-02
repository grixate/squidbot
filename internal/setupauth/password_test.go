package setupauth

import (
	"strings"
	"testing"
)

func TestGeneratePassword(t *testing.T) {
	first, err := GeneratePassword()
	if err != nil {
		t.Fatalf("generate password failed: %v", err)
	}
	second, err := GeneratePassword()
	if err != nil {
		t.Fatalf("generate password failed: %v", err)
	}
	if len(first) != generatedPasswordLength {
		t.Fatalf("expected generated password length %d, got %d", generatedPasswordLength, len(first))
	}
	if first == second {
		t.Fatal("expected generated passwords to differ")
	}
	for _, char := range first {
		if !strings.ContainsRune(passwordAlphabet, char) {
			t.Fatalf("generated password contains unexpected character %q", char)
		}
	}
}

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("very-secure-password")
	if err != nil {
		t.Fatalf("hash failed: %v", err)
	}
	if !VerifyPassword("very-secure-password", hash) {
		t.Fatal("expected password hash to verify")
	}
	if VerifyPassword("wrong-password", hash) {
		t.Fatal("expected mismatched password to fail")
	}
}

func TestHashPasswordRejectsEmpty(t *testing.T) {
	if _, err := HashPassword("   "); err == nil {
		t.Fatal("expected empty password hash to fail")
	}
}
