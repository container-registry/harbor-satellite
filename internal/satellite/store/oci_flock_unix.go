//go:build unix

package store

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockLayout takes an exclusive, non-blocking lock on the layout so two
// satellites cannot share one root and delete each other's staging copies.
// The lock lives as long as the returned file stays open.
func lockLayout(root string) (*os.File, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create OCI store root %s: %w", root, err)
	}
	f, err := os.OpenFile(filepath.Join(root, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open OCI store lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("OCI store %s is in use by another satellite; set a distinct --registry-data-dir: %w", root, err)
	}
	return f, nil
}
