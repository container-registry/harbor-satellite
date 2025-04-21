package state

import (
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/utils"
	"github.com/rs/zerolog"
)

func getStateFetcherForInput(input, username, password string, useInsecure bool, log *zerolog.Logger) (StateFetcher, error) {
	if utils.IsValidURL(input) {
		log.Info().Msg("Input is a valid URL")
		return NewURLStateFetcher(input, username, password, useInsecure), nil
	}

	log.Info().Msg("Input is not a valid URL, checking if it is a file path")
	if err := validateFilePath(input, log); err != nil {
		return nil, err
	}

	return processFileInput(input, username, password, log)
}

func validateFilePath(path string, log *zerolog.Logger) error {
	if utils.HasInvalidPathChars(path) {
		log.Error().Msg("Path contains invalid characters")
		return fmt.Errorf("invalid file path: %s", path)
	}
	if err := utils.GetAbsFilePath(path); err != nil {
		log.Error().Err(err).Msg("No file found")
		return fmt.Errorf("no file found: %s", path)
	}
	return nil
}

func processFileInput(input, username, password string, log *zerolog.Logger) (StateFetcher, error) {
	log.Info().Msg("Input is a valid file path")
	stateArtifactFetcher := NewFileStateFetcher(input, username, password)
	return stateArtifactFetcher, nil
}
