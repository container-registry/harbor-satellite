package state

import (
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/utils"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
)

func getStateFetcherForInput(input, username, password string, useInsecure bool, log *zerolog.Logger, cm *config.ConfigManager) (StateFetcher, error) {
	if !utils.IsValidURL(input) {
		log.Error().Msg("Input is not a valid URL")
		return nil, fmt.Errorf("invalid state url provided: %s", input)
	}
	log.Info().Msg("Input is a valid URL")
	return NewURLStateFetcher(input, username, password, useInsecure, cm), nil
}
