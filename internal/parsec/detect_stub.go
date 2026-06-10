//go:build !parsec

package parsec

import "errors"

// ErrParsecNotAvailable is returned by all operations in non-parsec builds.
var ErrParsecNotAvailable = errors.New("PARSEC support not compiled in this build (rebuild with -tags parsec)")

// Detect always returns false in non-parsec builds.
func Detect(_ string) bool { return false }

// MustDetect always returns an error in non-parsec builds.
func MustDetect(_ string) error { return ErrParsecNotAvailable }
