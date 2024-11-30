package state

import (
	"fmt"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/utils"
	"github.com/rs/zerolog"
)

func processInput(input, username, password string, log *zerolog.Logger) (StateFetcher, error) {

	if utils.IsValidURL(input) {
		return processURLInput(utils.FormatRegistryURL(input), username, password, log)
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

func processURLInput(input, username, password string, log *zerolog.Logger) (StateFetcher, error) {
	log.Info().Msg("Input is a valid URL")
	config.SetSourceRegistryURL(input)

	stateArtifactFetcher := NewURLStateFetcher(input, username, password)

	return stateArtifactFetcher, nil
}

func processFileInput(input, username, password string, log *zerolog.Logger) (StateFetcher, error) {
	log.Info().Msg("Input is a valid file path")
	stateArtifactFetcher := NewFileStateFetcher(input, username, password)
	return stateArtifactFetcher, nil
}
