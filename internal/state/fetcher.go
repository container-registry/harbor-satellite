package state

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/container-registry/harbor-satellite/internal/config"
	"github.com/container-registry/harbor-satellite/internal/utils"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/rs/zerolog"
)

type StateFetcher interface {
	FetchStateArtifact(ctx context.Context, state interface{}, log *zerolog.Logger) error
}

type baseStateFetcher struct {
	username string
	password string
}

type URLStateFetcher struct {
	baseStateFetcher
	url string
}

type FileStateArtifactFetcher struct {
	baseStateFetcher
	filePath string
}

func NewURLStateFetcher(stateURL, userName, password string) StateFetcher {
	url := utils.FormatRegistryURL(stateURL)
	return &URLStateFetcher{
		baseStateFetcher: baseStateFetcher{
			username: userName,
			password: password,
		},
		url: url,
	}
}

func NewFileStateFetcher(filePath, userName, password string) StateFetcher {
	return &FileStateArtifactFetcher{
		baseStateFetcher: baseStateFetcher{
			username: userName,
			password: password,
		},
		filePath: filePath,
	}
}

// TODO: Do we need the file state artifact fetcher?
func (f *FileStateArtifactFetcher) FetchStateArtifact(ctx context.Context, state interface{}, log *zerolog.Logger) error {
	content, err := os.ReadFile(f.filePath)
	if err != nil {
		return fmt.Errorf("failed to read the state artifact file: %v", err)
	}
	err = json.Unmarshal(content, state)
	if err != nil {
		return fmt.Errorf("failed to parse the state artifact file: %v", err)
	}
	return nil
}

func (f *URLStateFetcher) FetchStateArtifact(ctx context.Context, state interface{}, log *zerolog.Logger) error {
	switch s := state.(type) {
	case *SatelliteState:
		return f.fetchSatelliteState(ctx, s, log)

	case *State:
		return f.fetchGroupState(ctx, s, log)

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

func (f *URLStateFetcher) pullImage(ctx context.Context, log *zerolog.Logger) (v1.Image, error) {
	log.Debug().Msgf("Pulling state artifact: %s", f.url)
	auth := authn.FromConfig(authn.AuthConfig{
		Username: f.username,
		Password: f.password,
	})
	options := []crane.Option{crane.WithAuth(auth), crane.WithContext(ctx)}
	if config.UseUnsecure() {
		options = append(options, crane.Insecure)
	}
	return crane.Pull(f.url, options...)
}

func (f *URLStateFetcher) extractArtifactJSON(url string, img v1.Image, out interface{}, log *zerolog.Logger) error {
	log.Debug().Msgf("Extracting artifact.json from the state artifact: %s", url)

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
