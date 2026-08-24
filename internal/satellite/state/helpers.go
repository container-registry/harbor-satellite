package state

import (
	"fmt"
	"strings"

	"github.com/container-registry/harbor-satellite/internal/utils"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/rs/zerolog"
)

func getStateFetcherForInput(input, username, password string, useInsecure bool, log *zerolog.Logger) (StateFetcher, error) {
	return getStateFetcherForInputWithTLS(input, username, password, useInsecure, config.TLSConfig{}, log)
}

func getStateFetcherForInputWithTLS(input, username, password string, useInsecure bool, tlsCfg config.TLSConfig, log *zerolog.Logger) (StateFetcher, error) {
	if !utils.IsValidURL(input) {
		log.Error().Msg("Input is not a valid URL")
		return nil, fmt.Errorf("invalid state url provided: %s", input)
	}
	log.Info().Msg("Input is a valid URL")

	return NewURLStateFetcherWithTLS(input, username, password, useInsecure, tlsCfg), nil
}

func stateConfigFromResponse(response groundcontrol.StateConfigResponse) config.StateConfig {
	return config.StateConfig{
		RegistryCredentials: config.RegistryCredentials{
			URL:      config.URL(response.Auth.URL),
			Username: response.Auth.Username,
			Password: response.Auth.Password,
		},
		StateURL: response.State,
	}
}

func responseError(operation, status string, response *groundcontrol.AppError) error {
	return fmt.Errorf("%s: %s: code=%d message=%q", operation, status, response.Code, response.Message)
}

func unknownResponseError(operation, status string, body []byte) error {
	return fmt.Errorf("%s: unexpected response status=%q body=%q", operation, status, strings.TrimSpace(string(body)))
}
