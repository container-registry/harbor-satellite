package crypto

import (
	"strings"
	"testing"
)

func TestHashSecret(t *testing.T) {
	tests := []struct {
		name   string
		secret string
	}{
		{"basic", "password123"},
		{"empty", ""},
		{"long", strings.Repeat("a", 256)},
		{"unicode", "пароль🔐"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := HashSecret(tt.secret)
			if err != nil {
				t.Fatalf("HashSecret failed: %v", err)
			}
			if !strings.HasPrefix(hash, "$argon2id$") {
				t.Errorf("hash should start with $argon2id$, got %s", hash)
			}
		})
	}

	t.Run("unique salts", func(t *testing.T) {
		h1, _ := HashSecret("same")
		h2, _ := HashSecret("same")
		if h1 == h2 {
			t.Error("same secret should produce different hashes due to random salt")
		}
	})
}

func TestVerifySecret(t *testing.T) {
	secret := "test-password"
	hash, err := HashSecret(secret)
	if err != nil {
		t.Fatalf("HashSecret failed: %v", err)
	}

	tests := []struct {
		name   string
		secret string
		want   bool
	}{
		{"correct", secret, true},
		{"wrong", "wrong-password", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VerifySecret(tt.secret, hash)
			if got != tt.want {
				t.Errorf("VerifySecret(%q) = %v, want %v", tt.secret, got, tt.want)
			}
		})
	}

	t.Run("malformed hash", func(t *testing.T) {
		if VerifySecret(secret, "not-a-hash") {
			t.Error("malformed hash should return false")
		}
	})
}
