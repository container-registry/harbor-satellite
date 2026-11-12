package budget

import (
	"fmt"

	"github.com/container-registry/harbor-satellite/pkg/config"
)

// PolicyFrom turns the validated quota block into a live policy.
func PolicyFrom(cfg config.NetworkConfig) (Policy, error) {
	var policy Policy
	if !cfg.Quota.Enabled {
		return policy, nil
	}
	limit, err := config.ParseByteSize(cfg.Quota.Limit)
	if err != nil {
		return Policy{}, fmt.Errorf("network quota: %w", err)
	}
	window, err := parseWindow(cfg.Quota.Window)
	if err != nil {
		return Policy{}, err
	}
	policy.Quota = Quota{Enabled: true, LimitBytes: limit, Window: window}
	return policy, nil
}

// Apply installs the quota block on the process budget.
func Apply(cfg config.NetworkConfig) error {
	policy, err := PolicyFrom(cfg)
	if err != nil {
		return err
	}
	Ensure().SetPolicy(policy)
	return nil
}

func parseWindow(raw string) (Window, error) {
	switch Window(raw) {
	case WindowSync, WindowDay, WindowWeek:
		return Window(raw), nil
	default:
		return "", fmt.Errorf("network quota window %q must be sync, day, or week", raw)
	}
}
