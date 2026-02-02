package state

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/container-registry/harbor-satellite/internal/utils"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/rs/zerolog"
	"strings"
)

type StateFetcher interface {
	FetchStateArtifact(ctx context.Context, state interface{}, log *zerolog.Logger) error
	FetchDigest(ctx context.Context, log *zerolog.Logger) (string, error)
}

type baseStateFetcher struct {
	username string
	password string
}

type URLStateFetcher struct {
	baseStateFetcher
	url      string
	insecure bool
	cm       *config.ConfigManager
}

func NewURLStateFetcher(stateURL, userName, password string, insecure bool, cm *config.ConfigManager) StateFetcher {
	url := utils.FormatRegistryURL(stateURL)
	return &URLStateFetcher{
		baseStateFetcher: baseStateFetcher{
			username: userName,
			password: password,
		},
		url:      url,
		insecure: insecure,
		cm:       cm,
	}
}

func (f *URLStateFetcher) FetchStateArtifact(ctx context.Context, state interface{}, log *zerolog.Logger) error {
	switch s := state.(type) {
	case *SatelliteState:
		return f.fetchSatelliteState(ctx, s, log)

	case *State:
		return f.fetchGroupState(ctx, s, log)

	case *config.Config:
		return f.fetchConfigState(ctx, s, log)

	default:
		return fmt.Errorf("unexpected state type: %T", s)
	}
}

func (f *URLStateFetcher) fetchSatelliteState(ctx context.Context, state *SatelliteState, log *zerolog.Logger) error {
	log.Info().Msgf("Fetching satellite state artifact: %s", f.url)
	img, err := f.pullImage(ctx, log)
	if err != nil {
		return err
	}
	return f.extractArtifactJSON(f.url, img, state, log)
}

func (f *URLStateFetcher) fetchGroupState(ctx context.Context, state *State, log *zerolog.Logger) error {
	log.Info().Msgf("Fetching group state artifact: %s", f.url)
	img, err := f.pullImage(ctx, log)
	if err != nil {
		return err
	}
	return f.extractArtifactJSON(f.url, img, state, log)
}

func (f *URLStateFetcher) fetchConfigState(ctx context.Context, config *config.Config, log *zerolog.Logger) error {
	log.Info().Msgf("Fetching config state artifact: %s", f.url)
	img, err := f.pullImage(ctx, log)
	if err != nil {
		return err
	}
	return f.extractArtifactJSON(f.url, img, config, log)
}

func (f *URLStateFetcher) FetchDigest(ctx context.Context, log *zerolog.Logger) (string, error) {
	log.Debug().Msgf("Fetching digest for state artifact: %s", f.url)
	return f.fetchDigestWithRetry(ctx, log, 1) // 1 retry allowed
}


func (f *URLStateFetcher) tryRefreshCredentials(ctx context.Context, log *zerolog.Logger) error {
	log.Warn().Msg("Authentication failed, attempting to refresh credentials")
	if f.cm == nil {
		log.Error().Msg("ConfigManager is nil, cannot refresh credentials")
		return fmt.Errorf("config manager is nil")
	}
	if err := f.cm.RefreshCredentials(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to refresh credentials")
		return err
	}
	f.password = f.cm.GetSourceRegistryPassword()
	f.username = f.cm.GetSourceRegistryUsername()
	return nil
}

func (f *URLStateFetcher) fetchDigestWithRetry(ctx context.Context, log *zerolog.Logger, retries int) (string, error) {
	auth := authn.FromConfig(authn.AuthConfig{
		Username: f.username,
		Password: f.password,
	})
	options := []crane.Option{crane.WithAuth(auth), crane.WithContext(ctx)}
	if f.insecure {
		options = append(options, crane.Insecure)
	}

	digest, err := crane.Digest(f.url, options...)
	if err != nil && retries > 0 {
		if isAuthError(err.Error()) {
			if refreshErr := f.tryRefreshCredentials(ctx, log); refreshErr == nil {
				return f.fetchDigestWithRetry(ctx, log, retries-1)
			}
			return "", err
		}
	}
	return digest, err
}

func (f *URLStateFetcher) pullImage(ctx context.Context, log *zerolog.Logger) (v1.Image, error) {
	log.Debug().Msgf("Pulling state artifact: %s", f.url)
	return f.pullImageWithRetry(ctx, log, 1)
}

func (f *URLStateFetcher) pullImageWithRetry(ctx context.Context, log *zerolog.Logger, retries int) (v1.Image, error) {
	auth := authn.FromConfig(authn.AuthConfig{
		Username: f.username,
		Password: f.password,
	})
	options := []crane.Option{crane.WithAuth(auth), crane.WithContext(ctx)}
	if f.insecure {
		options = append(options, crane.Insecure)
	}
	img, err := crane.Pull(f.url, options...)
	if err != nil && retries > 0 {
		if isAuthError(err.Error()) {
			if refreshErr := f.tryRefreshCredentials(ctx, log); refreshErr == nil {
				return f.pullImageWithRetry(ctx, log, retries-1)
			}
			return nil, err
		}
	}
	return img, err
}

func isAuthError(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "unauthorized") || 
	       strings.Contains(msg, "forbidden") || 
	       strings.Contains(msg, "status code 401") || 
	       strings.Contains(msg, "status code 403")
}

func (f *URLStateFetcher) extractArtifactJSON(url string, img v1.Image, out interface{}, log *zerolog.Logger) error {
	log.Debug().Msgf("Extracting artifacts.json from the state artifact: %s", url)

	tarContent := new(bytes.Buffer)
	if err := crane.Export(img, tarContent); err != nil {
		log.Error().Msgf("Error exporting the fs contents of the state artifact: %s", url)
		return fmt.Errorf("failed to export the state artifact: %v", err)
	}

	tr := tar.NewReader(tarContent)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Error().Msgf("Failed to read the tar archive of the state artifact: %s", url)
			return fmt.Errorf("failed to read the tar archive: %v", err)
		}

		if hdr.Name == "artifacts.json" {
			artifactsJSON, err := io.ReadAll(tr)
			if err != nil {
				log.Error().Msgf("Failed to read the artifacts.json of the state artifact: %s", url)
				return fmt.Errorf("failed to read the artifacts.json file: %v", err)
			}
			return json.Unmarshal(artifactsJSON, out)
		}
	}
	log.Error().Msgf("artifacts.json not present for the state artifact: %s", url)
	return fmt.Errorf("artifacts.json not found in the state artifact")
}

func FromJSON(data []byte, reg StateReader) (StateReader, error) {
	if err := json.Unmarshal(data, &reg); err != nil {
		fmt.Print("Error in unmarshalling")
		return nil, err
	}
	if reg.GetRegistryURL() == "" {
		return nil, fmt.Errorf("registry URL is required")
	}
	return reg, nil
}
