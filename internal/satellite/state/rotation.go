package state

import (
	"github.com/container-registry/harbor-satellite/internal/satellite/gcclient"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
	"github.com/container-registry/harbor-satellite/pkg/config"
)

// ApplyRotatedCredentials switches the satellite to a successor robot
// delivered with a heartbeat (plan item 8.4): it stores the credentials
// and their generation and writes config.json, so the next status report
// acks the generation.
func ApplyRotatedCredentials(cm *config.ConfigManager, rotated gcclient.RotatedCredentials) error {
	return notimpl.Err("8.4 apply rotated credentials")
}
