// Package notimpl marks skeleton code that a later change implements.
package notimpl

import (
	"errors"
	"fmt"
)

// ErrNotImplemented is returned, wrapped, by every skeleton stub.
var ErrNotImplemented = errors.New("not implemented")

// Err wraps ErrNotImplemented with the plan item, e.g. Err("2.3 publisher").
func Err(item string) error {
	return fmt.Errorf("%w: %s", ErrNotImplemented, item)
}
