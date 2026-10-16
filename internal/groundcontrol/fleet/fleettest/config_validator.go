package fleettest

import (
	"context"
	"encoding/json"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ fleet.ConfigValidator = (*ConfigValidator)(nil)

// ConfigValidator is a recording fleet.ConfigValidator that answers with
// Warnings and Err.
type ConfigValidator struct {
	*Recorder
	Warnings []string
	Err      error
}

// NewConfigValidator accepts every document until the test sets Err; it
// records into rec, or into a new Recorder when rec is nil.
func NewConfigValidator(rec *Recorder) *ConfigValidator {
	return &ConfigValidator{Recorder: orNew(rec)}
}

func (v *ConfigValidator) ValidateConfig(_ context.Context, document json.RawMessage) ([]string, error) {
	if err := v.Record("ValidateConfig", document); err != nil {
		return nil, err
	}
	return v.Warnings, v.Err
}
