//go:build !nospiffe

package spiffe

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// EmbeddedSpireConfig holds configuration for the embedded SPIRE server.
type EmbeddedSpireConfig struct {
	Enabled     bool
	DataDir     string
	TrustDomain string
	BindAddress string
	BindPort    int
}

// EmbeddedSpireServer manages a SPIRE server subprocess.
type EmbeddedSpireServer struct {
	config     *EmbeddedSpireConfig
	cmd        *exec.Cmd
	client     *ServerClient
	socketPath string
	configPath string
}

// NewEmbeddedSpireServer creates a new embedded SPIRE server manager.
func NewEmbeddedSpireServer(cfg *EmbeddedSpireConfig) *EmbeddedSpireServer {
	socketPath := filepath.Join(cfg.DataDir, "spire-server.sock")
	configPath := filepath.Join(cfg.DataDir, "server.conf")

	return &EmbeddedSpireServer{
		config:     cfg,
		socketPath: socketPath,
		configPath: configPath,
	}
}

// Start starts the embedded SPIRE server subprocess and waits for it to be ready.
func (s *EmbeddedSpireServer) Start(ctx context.Context) error {
	if err := os.MkdirAll(s.config.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	if err := s.writeConfig(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	s.cmd = exec.CommandContext(ctx, "spire-server", "run", "-config", s.configPath)
	s.cmd.Stdout = os.Stdout
	s.cmd.Stderr = os.Stderr

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("start spire-server: %w", err)
	}

	log.Printf("Embedded SPIRE server started (PID: %d)", s.cmd.Process.Pid)

	if err := s.waitForReady(ctx); err != nil {
		s.Stop()
		return fmt.Errorf("wait for server ready: %w", err)
	}

	client, err := NewServerClient(s.socketPath, s.config.TrustDomain)
	if err != nil {
		s.Stop()
		return fmt.Errorf("create server client: %w", err)
	}
	s.client = client

	log.Printf("Embedded SPIRE server ready (trust domain: %s)", s.config.TrustDomain)
	return nil
}

// Stop gracefully stops the SPIRE server subprocess.
func (s *EmbeddedSpireServer) Stop() error {
	if s.client != nil {
		s.client.Close()
		s.client = nil
	}

	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}

	// Send SIGTERM for graceful shutdown
	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		log.Printf("Failed to send SIGTERM to SPIRE server: %v", err)
	}

	// Wait for process to exit (with timeout)
	done := make(chan error, 1)
	go func() {
		done <- s.cmd.Wait()
	}()

	select {
	case <-time.After(10 * time.Second):
		log.Println("SPIRE server shutdown timeout, sending SIGKILL")
		s.cmd.Process.Kill()
		<-done
	case err := <-done:
		if err != nil {
			log.Printf("SPIRE server exited with error: %v", err)
		}
	}

	log.Println("Embedded SPIRE server stopped")
	return nil
}

// GetClient returns the SPIRE Server API client.
func (s *EmbeddedSpireServer) GetClient() *ServerClient {
	return s.client
}

// GetTrustDomain returns the configured trust domain.
func (s *EmbeddedSpireServer) GetTrustDomain() string {
	return s.config.TrustDomain
}

// GetSocketPath returns the SPIRE server socket path.
func (s *EmbeddedSpireServer) GetSocketPath() string {
	return s.socketPath
}

// GetBindPort returns the TCP port for agent connections.
func (s *EmbeddedSpireServer) GetBindPort() int {
	return s.config.BindPort
}

// waitForReady polls the socket until the server is ready.
func (s *EmbeddedSpireServer) waitForReady(ctx context.Context) error {
	deadline := time.Now().Add(60 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for SPIRE server")
			}

			// Check if socket exists
			if _, err := os.Stat(s.socketPath); err == nil {
				// Try to connect
				client, err := NewServerClient(s.socketPath, s.config.TrustDomain)
				if err == nil {
					client.Close()
					return nil
				}
			}
		}
	}
}

// writeConfig writes the SPIRE server configuration file.
func (s *EmbeddedSpireServer) writeConfig() error {
	config := fmt.Sprintf(`server {
    bind_address = "%s"
    bind_port = "%d"
    socket_path = "%s"
    trust_domain = "%s"
    data_dir = "%s"
    log_level = "INFO"
}

plugins {
    DataStore "sql" {
        plugin_data {
            database_type = "sqlite3"
            connection_string = "%s/datastore.sqlite3"
        }
    }

    NodeAttestor "join_token" {
        plugin_data {}
    }

    KeyManager "memory" {
        plugin_data {}
    }
}
`,
		s.config.BindAddress,
		s.config.BindPort,
		s.socketPath,
		s.config.TrustDomain,
		s.config.DataDir,
		s.config.DataDir,
	)

	return os.WriteFile(s.configPath, []byte(config), 0o600)
}
