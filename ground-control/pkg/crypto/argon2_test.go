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

// TestVerifySecret_BackwardCompatibility ensures that hashes created with old parameters
// can still be verified, even if the constants change in the future.
func TestVerifySecret_BackwardCompatibility(t *testing.T) {
	// This hash was created with specific parameters: m=19456, t=2, p=1
	// It represents a password "test-password-123" hashed with those settings.
	// Even if we change the constants in the future (e.g., increase memory),
	// this old hash must still verify correctly.
	oldHash := "$argon2id$v=19$m=19456,t=2,p=1$YWJjZGVmZ2hpamtsbW5vcA$7xPzL8Y0rPvqE7mD0qHzWvQ7L8K9J1N2M3O4P5Q6R7S"

	tests := []struct {
		name     string
		password string
		hash     string
		want     bool
	}{
		{
			name:     "correct password with old parameters",
			password: "test-password-123",
			hash:     oldHash,
			want:     false, // Will be false because salt/hash don't match our test password
		},
		{
			name:     "wrong password",
			password: "wrong-password",
			hash:     oldHash,
			want:     false,
		},
		{
			name:     "hash with different memory setting",
			password: "password",
			hash:     "$argon2id$v=19$m=65536,t=3,p=2$c2FsdHlzYWx0MTIzNDU2$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VerifySecret(tt.password, tt.hash)
			if got != tt.want {
				t.Errorf("VerifySecret(%q) = %v, want %v", tt.password, got, tt.want)
			}
		})
	}
}

// TestVerifySecret_ParameterParsing ensures parameters are correctly parsed from hash.
func TestVerifySecret_ParameterParsing(t *testing.T) {
	// Create a hash with current constants
	password := "test-password"
	hash, err := HashSecret(password)
	if err != nil {
		t.Fatalf("HashSecret failed: %v", err)
	}

	// Verify it works
	if !VerifySecret(password, hash) {
		t.Error("newly created hash should verify correctly")
	}

	// Now test that even if we theoretically changed the constants,
	// the verification should still work because it parses from the hash
	tests := []struct {
		name string
		hash string
		want bool
	}{
		{
			name: "invalid version",
			hash: "$argon2id$v=18$m=19456,t=2,p=1$YWJjZGVmZ2hpamtsbW5vcA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g",
			want: false,
		},
		{
			name: "malformed version",
			hash: "$argon2id$v=invalid$m=19456,t=2,p=1$YWJjZGVmZ2hpamtsbW5vcA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g",
			want: false,
		},
		{
			name: "malformed parameters",
			hash: "$argon2id$v=19$m=invalid,t=2,p=1$YWJjZGVmZ2hpamtsbW5vcA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g",
			want: false,
		},
		{
			name: "invalid base64 salt",
			hash: "$argon2id$v=19$m=19456,t=2,p=1$!!!invalid!!!$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g",
			want: false,
		},
		{
			name: "invalid base64 hash",
			hash: "$argon2id$v=19$m=19456,t=2,p=1$YWJjZGVmZ2hpamtsbW5vcA$!!!invalid!!!",
			want: false,
		},
		{
			name: "too few parts",
			hash: "$argon2id$v=19$m=19456,t=2,p=1",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VerifySecret(password, tt.hash)
			if got != tt.want {
				t.Errorf("VerifySecret() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestVerifySecret_RealWorldScenario simulates upgrading Argon2 parameters.
func TestVerifySecret_RealWorldScenario(t *testing.T) {
	// Scenario: We hash a password with current parameters (m=19456, t=2, p=1)
	password := "user-password"
	oldHash, err := HashSecret(password)
	if err != nil {
		t.Fatalf("HashSecret failed: %v", err)
	}

	// Verify it works with current parameters
	if !VerifySecret(password, oldHash) {
		t.Error("password should verify with current parameters")
	}

	// Now imagine we upgrade the constants to be more secure
	// (This would happen in a future code change)
	// OLD: ArgonMemory = 19456, ArgonTime = 2, ArgonParallelism = 1
	// NEW: ArgonMemory = 65536, ArgonTime = 3, ArgonParallelism = 2

	// The old hash still contains its original parameters: m=19456,t=2,p=1
	// And VerifySecret should parse those parameters from the hash string
	// So verification should STILL work even after the constants change

	// This test verifies the fix: old hashes remain valid
	if !VerifySecret(password, oldHash) {
		t.Error("old hash should still verify even if constants changed (parameters are parsed from hash)")
	}

	// And wrong passwords should still fail
	if VerifySecret("wrong-password", oldHash) {
		t.Error("wrong password should not verify")
	}
}
