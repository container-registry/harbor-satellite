package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/container-registry/harbor-satellite/internal/crypto"
	"github.com/container-registry/harbor-satellite/internal/identity"
	"github.com/container-registry/harbor-satellite/internal/secure"
	"github.com/container-registry/harbor-satellite/internal/token"
)

type SampleConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Token    string `json:"token"`
}

func main() {
	fmt.Println("=== Phase 1 Verification ===")

	// 1. Test Device Identity
	fmt.Println("## 1. Device Identity")
	device := identity.NewLinuxDeviceIdentity()

	fingerprint, err := device.GetFingerprint()
	if err != nil {
		fmt.Printf("   Fingerprint: ERROR - %v\n", err)
	} else {
		fmt.Printf("   Fingerprint: %s\n", fingerprint)
	}

	machineID, _ := device.GetMachineID()
	fmt.Printf("   Machine ID:  %s\n", machineID)

	mac, _ := device.GetMACAddress()
	fmt.Printf("   MAC Address: %s\n", mac)

	bootID, _ := device.GetBootID()
	fmt.Printf("   Boot ID:     %s\n", bootID)

	fmt.Println()

	// 2. Test Crypto Provider
	fmt.Println("## 2. Crypto Provider")
	cryptoProvider := crypto.NewAESProvider()

	plaintext := []byte("secret-data-123")
	key := []byte("test-encryption-key")

	encrypted, err := cryptoProvider.Encrypt(plaintext, key)
	if err != nil {
		fmt.Printf("   Encrypt: ERROR - %v\n", err)
		return
	}
	fmt.Printf("   Original:  %s\n", string(plaintext))
	fmt.Printf("   Encrypted: %x... (%d bytes)\n", encrypted[:16], len(encrypted))

	decrypted, err := cryptoProvider.Decrypt(encrypted, key)
	if err != nil {
		fmt.Printf("   Decrypt: ERROR - %v\n", err)
		return
	}
	fmt.Printf("   Decrypted: %s\n", string(decrypted))
	fmt.Printf("   Match:     %v\n", string(plaintext) == string(decrypted))

	fmt.Println()

	// 3. Test Config Encryption
	fmt.Println("## 3. Config Encryption (Device-Bound)")
	encryptor := secure.NewConfigEncryptor(cryptoProvider, device)

	config := SampleConfig{
		Username: "admin",
		Password: "super-secret-password",
		Token:    "jwt-token-xyz",
	}

	encryptedConfig, err := encryptor.EncryptConfig(config)
	if err != nil {
		fmt.Printf("   Encrypt Config: ERROR - %v\n", err)
		return
	}

	fmt.Printf("   Original Config:\n")
	origJSON, _ := json.MarshalIndent(config, "      ", "  ")
	fmt.Printf("      %s\n", string(origJSON))

	fmt.Printf("   Encrypted (first 100 chars):\n")
	if len(encryptedConfig) > 100 {
		fmt.Printf("      %s...\n", string(encryptedConfig[:100]))
	} else {
		fmt.Printf("      %s\n", string(encryptedConfig))
	}

	// Verify credentials not visible
	containsPassword := string(encryptedConfig)
	if contains(containsPassword, "super-secret-password") {
		fmt.Printf("   Security Check: FAILED - password visible in encrypted data!\n")
	} else {
		fmt.Printf("   Security Check: PASSED - password not visible\n")
	}

	// Decrypt and verify
	var decryptedConfig SampleConfig
	err = encryptor.DecryptConfig(encryptedConfig, &decryptedConfig)
	if err != nil {
		fmt.Printf("   Decrypt Config: ERROR - %v\n", err)
		return
	}
	fmt.Printf("   Roundtrip:  %v\n", config == decryptedConfig)

	fmt.Println()

	// 4. Test Join Token
	fmt.Println("## 4. Join Token")
	joinToken, encoded, err := token.GenerateToken("https://ground-control.example.com", 24*time.Hour)
	if err != nil {
		fmt.Printf("   Generate: ERROR - %v\n", err)
		return
	}

	fmt.Printf("   Token ID:     %s\n", joinToken.ID[:16]+"...")
	fmt.Printf("   Expires:      %s\n", joinToken.ExpiresAt.Format(time.RFC3339))
	fmt.Printf("   Ground URL:   %s\n", joinToken.GroundURL)
	fmt.Printf("   Encoded:      %s...\n", encoded[:50])

	// Validate
	err = joinToken.Validate()
	fmt.Printf("   Valid:        %v\n", err == nil)

	// Test expiry detection
	expiredToken := &token.JoinToken{
		Version:   1,
		ID:        "test",
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	err = expiredToken.Validate()
	fmt.Printf("   Expired Check: %v (expected: token expired)\n", err)

	fmt.Println()

	// 5. Test Token Store (single-use)
	fmt.Println("## 5. Token Store (Single-Use)")
	store := token.NewMemoryTokenStore(5, time.Minute)

	err = store.MarkUsed("token-abc")
	fmt.Printf("   First use:  %v\n", err == nil)

	err = store.MarkUsed("token-abc")
	fmt.Printf("   Second use: %v (expected: already used)\n", err)

	fmt.Println()
	fmt.Println("=== Verification Complete ===")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
