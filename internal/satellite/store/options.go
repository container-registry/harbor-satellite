package store

import (
	"fmt"
	"strings"

	"github.com/container-registry/harbor-satellite/internal/shared/utils"
	"github.com/container-registry/harbor-satellite/pkg/config"
)

// ResolveSourceRegistry returns current Harbor source settings for proxy pulls
// and state replication.
func ResolveSourceRegistry(cm *config.ConfigManager) (RegistryOptions, error) {
	credentials := cm.GetSourceRegistryCredentials()
	sourceURL := string(credentials.URL)
	if override := cm.GetHarborRegistryURL(); override != "" {
		if sourceURL == "" {
			sourceURL = override
		} else {
			replaced, err := config.ReplaceURLHost(sourceURL, override)
			if err != nil {
				return RegistryOptions{}, fmt.Errorf("apply Harbor registry override: %w", err)
			}
			sourceURL = replaced
		}
	}
	return RegistryOptions{
		Endpoint:  utils.FormatRegistryURL(sourceURL),
		Username:  credentials.Username,
		Password:  credentials.Password,
		PlainHTTP: cm.UseUnsecure() || strings.HasPrefix(strings.ToLower(sourceURL), "http://"),
		TLS:       cm.GetTLSConfig(),
	}, nil
}
