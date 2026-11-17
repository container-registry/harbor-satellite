// Package satconfig validates satellite config documents for Ground
// Control with pkg/config's rules (plan item 7.4). It implements
// fleet.ConfigValidator without importing fleet; the composition root
// asserts that.
package satconfig

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/container-registry/harbor-satellite/pkg/config"
)

type Validator struct{}

// ValidateConfig decodes document, a config document with app_config and
// state_config, into config.Config and runs config.ValidateAppConfig on its
// app_config. A document that does not decode is an error.
func (Validator) ValidateConfig(_ context.Context, document json.RawMessage) ([]string, error) {
	var cfg config.Config
	if err := json.Unmarshal(document, &cfg); err != nil {
		return nil, fmt.Errorf("decode config document: %w", err)
	}
	return config.ValidateAppConfig(cfg.AppConfig)
}
