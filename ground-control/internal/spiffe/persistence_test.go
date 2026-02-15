package spiffe

import (
	"context"
	"fmt"
	"os"
	"os/exec" // Added this for LookPath
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupTestConfig eliminates code duplication
func setupTestConfig(t *testing.T) *EmbeddedSpireConfig {
	// t.TempDir() automatically cleans up
	tmpDir := t.TempDir()

	return &EmbeddedSpireConfig{
		Enabled:     true,
		DataDir:     tmpDir,
		TrustDomain: "example.org",
		BindAddress: "127.0.0.1",
		BindPort:    0, // 0 lets OS pick random free port
	}
}

func TestSpireConfigUsesDiskPersistence(t *testing.T) {
	// 1. Setup
	cfg := setupTestConfig(t)

	// Create DataDir explicitly
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		t.Fatal(err)
	}

	server := NewEmbeddedSpireServer(cfg)

	// 2. Trigger config writing
	// (Ensure 'writeConfig' is capitalized/exported in your embedded_server.go if it isn't already,
	// OR just ensure this test is in package 'spiffe' to access unexported methods)
	if err := server.writeConfig(); err != nil {
		t.Fatal("Failed to write config:", err)
	}

	// 3. Read the file back
	configPath := filepath.Join(cfg.DataDir, "server.conf")
	contentBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(contentBytes)

	// 4. Verify Content
	if !strings.Contains(content, `KeyManager "disk"`) {
		t.Errorf("Config missing 'KeyManager \"disk\"'. Got:\n%s", content)
	}
	if !strings.Contains(content, `keys_path =`) {
		t.Errorf("Config missing 'keys_path'. Got:\n%s", content)
	}
}

func TestEndToEndPersistence(t *testing.T) {
	// 1. Setup
	cfg := setupTestConfig(t)

	// 2. Check if binary exists (Skip if missing)
	if _, err := exec.LookPath("spire-server"); err != nil {
		t.Skip("Skipping E2E test: 'spire-server' binary not found in PATH")
	}

	server1 := NewEmbeddedSpireServer(cfg)
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()

	// 3. Start Server 1
	go func() {
		if err := server1.Start(ctx1); err != nil {
			fmt.Printf("Server 1 stopped (expected): %v\n", err)
		}
	}()

	// Give it time to start
	time.Sleep(500 * time.Millisecond)

	// Stop it
	cancel1()
	time.Sleep(200 * time.Millisecond)

	// 4. Verify keys.json was created
	keysPath := filepath.Join(cfg.DataDir, "keys.json")
	if _, err := os.Stat(keysPath); os.IsNotExist(err) {
		t.Error("Failure: keys.json was NOT created. Persistence is broken.")
	} else {
		t.Log("Success: keys.json persists on disk.")
	}
}
