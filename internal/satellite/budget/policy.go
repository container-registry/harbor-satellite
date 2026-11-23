package budget

import (
	"fmt"

	"github.com/container-registry/harbor-satellite/pkg/config"
)

// PolicyFrom turns the validated speed block into a live policy.
func PolicyFrom(cfg config.NetworkConfig) (Policy, error) {
	var policy Policy
	if !cfg.Speed.Enabled {
		return policy, nil
	}
	perSecond, err := config.ParseByteRate(cfg.Speed.Limit)
	if err != nil {
		return Policy{}, fmt.Errorf("network speed: %w", err)
	}
	policy.Speed = Speed{Enabled: true, BytesPerSecond: perSecond}
	return policy, nil
}

// Apply installs the speed block on the process budget.
func Apply(cfg config.NetworkConfig) error {
	policy, err := PolicyFrom(cfg)
	if err != nil {
		return err
	}
	Ensure().SetPolicy(policy)
	return nil
}
