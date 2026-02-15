package spiffe

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
	// Note: os.MkdirAll is not needed here because t.TempDir() creates it.

	server := NewEmbeddedSpireServer(cfg)

	// 2. Trigger config writing
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

	// 4. Verify "KeyManager" is set to "disk"
	expectedKeyManager := `KeyManager "disk"`
	if !strings.Contains(content, expectedKeyManager) {
		t.Errorf("Config missing '%s'. Got:\n%s", expectedKeyManager, content)
	}

	// 5. Verify "keys_path" points to the correct location
	// We check the full path to be strict, as requested by review
	expectedKeysPath := fmt.Sprintf(`keys_path = "%s/keys.json"`, cfg.DataDir)
	if !strings.Contains(content, expectedKeysPath) {
		t.Errorf("Config has wrong keys_path. Expected %q in:\n%s", expectedKeysPath, content)
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

	// 3. Start Server 1 with Error Propagation
	errCh := make(chan error, 1)
	go func() {
		// We send the result of Start() to the channel
		errCh <- server1.Start(ctx1)
	}()

	// Wait for server to be ready or fail (No blind sleep!)
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Server 1 failed to start: %v", err)
		}
		// If err is nil, Start returned successfully (server is ready)
	case <-time.After(10 * time.Second):
		t.Fatal("Timed out waiting for Server 1 to start")
	}

	// 4. Graceful Stop
	// We must stop explicitly to ensure keys are flushed to disk
	if err := server1.Stop(); err != nil {
		t.Logf("Warning: Failed to stop server 1 gracefully: %v", err)
	}
	cancel1()

	// 5. Verify keys.json was created
	keysPath := filepath.Join(cfg.DataDir, "keys.json")
	if _, err := os.Stat(keysPath); os.IsNotExist(err) {
		t.Error("Failure: keys.json was NOT created. Persistence is broken.")
	} else {
		t.Log("Success: keys.json persists on disk.")
	}

	// 6. Restart Check (Server 2)
	// We want to make sure it can start up again using those existing keys
	server2 := NewEmbeddedSpireServer(cfg)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	go func() {
		errCh <- server2.Start(ctx2)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Server 2 failed to restart with persisted keys: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Timed out waiting for Server 2 to restart")
	}

	// Clean up Server 2
	_ = server2.Stop()
}
