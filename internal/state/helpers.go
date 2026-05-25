package state

import (
	"encoding/json"
	"fmt"
	"net/http"

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

// parseErrorResponse attempts to decode a JSON error message from the response body.
// It falls back to the HTTP status string if decoding fails.
func parseErrorResponse(resp *http.Response) string {
	var errResp struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Message != "" {
		return errResp.Message
	}
	return resp.Status
}

