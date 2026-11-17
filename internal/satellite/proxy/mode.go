package proxy

import "github.com/container-registry/harbor-satellite/pkg/config"

// Mode lives in pkg/config so internal/shared/env can parse PROXY_MODE
// without importing the satellite (go-arch-lint).
type Mode = config.ProxyMode

const (
	ModeProxy   = config.ProxyModeProxy
	ModeReplica = config.ProxyModeReplica
)

func ParseMode(value string) (Mode, error) {
	return config.ParseProxyMode(value)
}
