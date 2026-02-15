package spiffe

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSpireConfigUsesDiskPersistence verifies that the generated configuration file
// contains the correct instructions for disk persistence.
// This test runs in all environments (does not require spire-server binary).
func TestSpireConfigUsesDiskPersistence(t *testing.T) {
	// 1. Setup temp dir
	tmpDir, err := os.MkdirTemp("", "spire-config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// 2. Setup Config
	cfg := &EmbeddedSpireConfig{
		Enabled:     true,
		DataDir:     tmpDir,
		TrustDomain: "example.org",
		BindAddress: "127.0.0.1",
		BindPort:    8081,
	}

	server := NewEmbeddedSpireServer(cfg)

	// 3. Trigger config writing by attempting to start
	// We run this in a goroutine because Start() might block or fail if binary is missing
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		_ = server.Start(ctx)
	}()

	// Give it a tiny moment to write the file
	time.Sleep(100 * time.Millisecond)

	// 4. Read the generated 'server.conf'
	configPath := filepath.Join(tmpDir, "server.conf")

	// Retry loop in case file writing is slow
	var content string
	for i := 0; i < 5; i++ {
		contentBytes, err := os.ReadFile(configPath)
		if err == nil {
			content = string(contentBytes)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if content == "" {
		t.Fatalf("Failed to read server.conf from %s", configPath)
	}

	// 5. VERIFY the critical changes
	t.Log("Checking config content for persistence settings...")

	if !strings.Contains(content, `KeyManager "disk"`) {
		t.Errorf("FAIL: Config uses KeyManager memory! Expected 'disk'.\nContent:\n%s", content)
	} else {
		t.Log("PASS: Found KeyManager \"disk\"")
	}

	if !strings.Contains(content, `keys_path =`) {
		t.Errorf("FAIL: Config missing 'keys_path' setting.\nContent:\n%s", content)
	} else {
		t.Log("PASS: Found keys_path setting")
	}

	if !strings.Contains(content, "keys.json") {
		t.Errorf("FAIL: Config path does not point to keys.json.\nContent:\n%s", content)
	} else {
		t.Log("PASS: Found keys.json in path")
	}
}

// TestEndToEndPersistence performs a full integration test.
// It starts the server, checks for keys.json creation, stops, and restarts.
// It SKIPS if 'spire-server' binary is not found in PATH.
func TestEndToEndPersistence(t *testing.T) {
	// 1. SAFEGUARD: Skip if spire-server is not installed
	if _, err := exec.LookPath("spire-server"); err != nil {
		t.Skip("SKIPPING E2E Test: 'spire-server' binary not found in PATH")
	}

	// 2. Setup a persistent data directory for this test
	tmpDir, err := os.MkdirTemp("", "spire-e2e-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &EmbeddedSpireConfig{
		Enabled:     true,
		DataDir:     tmpDir,
		TrustDomain: "example.org",
		BindAddress: "127.0.0.1",
		BindPort:    0, // 0 lets OS pick random free port
	}

	keysPath := filepath.Join(tmpDir, "keys.json")

	// --- PHASE 1: FIRST BOOT ---
	t.Log("--- Starting Server (Run 1) ---")
	server1 := NewEmbeddedSpireServer(cfg)
	ctx1, cancel1 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel1()

	// Start in background
	go func() {
		if err := server1.Start(ctx1); err != nil {
			t.Logf("Server 1 stopped: %v", err)
		}
	}()

	// Wait for keys.json
	if err := waitForFile(ctx1, keysPath); err != nil {
		t.Fatalf("Run 1 Failed: keys.json never created: %v", err)
	}
	t.Log("SUCCESS: keys.json created")

	// Read the keys (Snapshot)
	originalKeys, err := os.ReadFile(keysPath)
	if err != nil {
		t.Fatal(err)
	}

	// Stop Server 1
	t.Log("--- Stopping Server (Run 1) ---")
	cancel1() // Cancel context
	_ = server1.Stop()

	// --- PHASE 2: RESTART ---
	t.Log("--- Restarting Server (Run 2) ---")

	// Create new server instance pointing to SAME data dir
	server2 := NewEmbeddedSpireServer(cfg)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()

	go func() {
		if err := server2.Start(ctx2); err != nil {
			t.Logf("Server 2 stopped: %v", err)
		}
	}()

	// Give it a moment to initialize
	time.Sleep(2 * time.Second)

	// --- PHASE 3: VERIFICATION ---

	// Check 1: Does the file still exist?
	newKeys, err := os.ReadFile(keysPath)
	if err != nil {
		t.Fatalf("Run 2 Failed: keys.json vanished! %v", err)
	}

	// Check 2: Are the keys identical?
	if !bytes.Equal(originalKeys, newKeys) {
		t.Errorf("FAILURE: Keys changed between restarts! Persistence failed.")
	} else {
		t.Log("SUCCESS: Keys persisted and are identical across restarts.")
	}

	_ = server2.Stop()
}

// Helper to wait for file creation
func waitForFile(ctx context.Context, path string) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if info, err := os.Stat(path); err == nil && info.Size() > 0 {
				return nil
			}
		}
	}
}
