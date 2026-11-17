package state

import (
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/satellite/gcclient"
	"github.com/container-registry/harbor-satellite/pkg/config"
)

// ApplyRotatedCredentials switches the satellite to a successor robot
// delivered with a heartbeat (plan item 8.4): it stores the credentials
// and their generation and writes config.json, so the next status report
// acks the generation.
func ApplyRotatedCredentials(cm *config.ConfigManager, rotated gcclient.RotatedCredentials) error {
	if rotated.Username == "" || rotated.Password == "" {
		return fmt.Errorf("rotated credentials of generation %d lack a username or password", rotated.Generation)
	}
	sc := cm.GetStateConfig()
	sc.RegistryCredentials.Username = rotated.Username
	sc.RegistryCredentials.Password = rotated.Password
	sc.CredentialsGeneration = rotated.Generation
	cm.With(config.SetStateConfig(sc))
	if err := cm.WriteConfig(); err != nil {
		return fmt.Errorf("persist rotated credentials: %w", err)
	}
	return nil
}
