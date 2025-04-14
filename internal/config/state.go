package config

import (
	"fmt"
)

func UpdateStates(states []string) error {
	if appConfig == nil {
		return fmt.Errorf("config not initialized")
	}

	appConfig.StateConfig.State = states[0]

	// Write the updated config to file
	if err := WriteConfig(DefaultConfigPath); err != nil {
		return fmt.Errorf("failed to write updated config: %v", err)
	}

	return nil
}

// GetStates returns the current states from the configuration
func GetStates() []string {
	if appConfig == nil {
		return nil
	}
	return []string{appConfig.StateConfig.State}
}
