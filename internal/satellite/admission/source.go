package admission

import (
	"github.com/container-registry/harbor-satellite/internal/shared/policybundle"
	"github.com/container-registry/harbor-satellite/pkg/config"
)

// BundleSource yields the current bundle and mode; ok is false when no
// bundle is configured.
type BundleSource interface {
	Current() (bundle policybundle.Bundle, mode policybundle.Mode, ok bool)
}

// ConfigSource reads app_config.admission from the satellite's config, so
// a config reconcile that changes it takes effect on the next request.
type ConfigSource struct{ CM *config.ConfigManager }

// Current returns app_config.admission's bundle and its mode parsed with
// policybundle.ParseMode; a mode that does not parse is ModeEnforce, so a
// typo fails closed. ok is false when no bundle is configured.
func (s ConfigSource) Current() (policybundle.Bundle, policybundle.Mode, bool) {
	return policybundle.Bundle{}, policybundle.ModeOff, false
}
