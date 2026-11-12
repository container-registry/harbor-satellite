// Package satconfig validates satellite config documents for Ground
// Control with pkg/config's rules (plan item 7.4). It implements
// fleet.ConfigValidator without importing fleet; the composition root
// asserts that.
package satconfig

import (
	"context"
	"encoding/json"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

type Validator struct{}

// ValidateConfig decodes document, a config document with app_config and
// state_config, into config.Config and runs config.ValidateAppConfig on its
// app_config. A document that does not decode is an error.
func (Validator) ValidateConfig(ctx context.Context, document json.RawMessage) ([]string, error) {
	return nil, notimpl.Err("7.4 config validation")
}
