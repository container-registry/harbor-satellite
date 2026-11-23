//go:build !unix

package store

import "os"

// lockLayout is a no-op where flock is unavailable.
func lockLayout(string) (*os.File, error) { return nil, nil }
