package fleet

import (
	"context"
	"encoding/json"
)

// ConfigValidator checks an app_config document before it is stored; it
// never rewrites the document: GC stores the merged document as submitted.
// The implementation lives outside fleet (satconfig), over pkg/config.
type ConfigValidator interface {
	ValidateConfig(ctx context.Context, document json.RawMessage) (warnings []string, err error)
}

// ConfigValidationError is answered 422 with the warnings; it wraps ErrInvalid.
type ConfigValidationError struct {
	Warnings []string
	Err      error
}

func (e *ConfigValidationError) Error() string { return e.Err.Error() }
func (e *ConfigValidationError) Unwrap() error { return e.Err }

// ConfigWriteResult is what Create and Update return: the stored config and
// the validation warnings, which are response data, not part of the entity.
type ConfigWriteResult struct {
	Config   Config
	Warnings []string
}
