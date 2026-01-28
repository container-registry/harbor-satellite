package state

import (
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/utils"
	"github.com/container-registry/harbor-satellite/pkg/config"
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
