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
	tmpDir := t.TempDir()
	return &EmbeddedSpireConfig{
		Enabled:     true,
		DataDir:     tmpDir,
		TrustDomain: "example.org",
		BindAddress: "127.0.0.1",
		BindPort:    0,
	}
}

// startServerAsync handles the complexity of starting a server in a goroutine
func startServerAsync(ctx context.Context, server *EmbeddedSpireServer) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(ctx)
	}()
	return errCh
}

// waitForServer waits for the server to start or fail
func waitForServer(t *testing.T, errCh <-chan error, timeout time.Duration, name string) {
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("%s failed to start: %v", name, err)
		}
	case <-time.After(timeout):
		t.Fatalf("Timed out waiting for %s to start", name)
	}
}

func TestSpireConfigUsesDiskPersistence(t *testing.T) {
	cfg := setupTestConfig(t)
	server := NewEmbeddedSpireServer(cfg)

	if err := server.writeConfig(); err != nil {
		t.Fatal("Failed to write config:", err)
	}

	configPath := filepath.Join(cfg.DataDir, "server.conf")
	contentBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(contentBytes)

	expectedKeyManager := `KeyManager "disk"`
	if !strings.Contains(content, expectedKeyManager) {
		t.Errorf("Config missing '%s'. Got:\n%s", expectedKeyManager, content)
	}

	expectedKeysPath := fmt.Sprintf(`keys_path = "%s/keys.json"`, cfg.DataDir)
	if !strings.Contains(content, expectedKeysPath) {
		t.Errorf("Config has wrong keys_path. Expected %q", expectedKeysPath)
	}
}

func TestEndToEndPersistence(t *testing.T) {
	if _, err := exec.LookPath("spire-server"); err != nil {
		t.Skip("Skipping E2E test: 'spire-server' binary not found in PATH")
	}

	cfg := setupTestConfig(t)

	// --- Run Server 1 ---
	server1 := NewEmbeddedSpireServer(cfg)
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()

	errCh1 := startServerAsync(ctx1, server1)
	waitForServer(t, errCh1, 10*time.Second, "Server 1")

	// Graceful Stop
	if err := server1.Stop(); err != nil {
		t.Logf("Warning: Failed to stop server 1 gracefully: %v", err)
	}
	cancel1()

	// Verify keys.json
	keysPath := filepath.Join(cfg.DataDir, "keys.json")
	if _, err := os.Stat(keysPath); os.IsNotExist(err) {
		t.Error("Failure: keys.json was NOT created. Persistence is broken.")
	}

	// --- Run Server 2 (Restart) ---
	server2 := NewEmbeddedSpireServer(cfg)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	errCh2 := startServerAsync(ctx2, server2)
	waitForServer(t, errCh2, 10*time.Second, "Server 2")

	_ = server2.Stop()
}
