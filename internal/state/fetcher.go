package state

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/utils"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
)

type StateFetcher interface {
	FetchStateArtifact(state interface{}) error
}

type baseStateFetcher struct {
	username              string
	password              string
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
			username:              userName,
			password:              password,
		},
		url: url,
	}
}

func NewFileStateFetcher(filePath, userName, password string) StateFetcher {
	return &FileStateArtifactFetcher{
		baseStateFetcher: baseStateFetcher{
			username:              userName,
			password:              password,
		},
		filePath: filePath,
	}
}

func (f *FileStateArtifactFetcher) FetchStateArtifact(state interface{}) error {
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

func (f *URLStateFetcher) FetchStateArtifact(state interface{}) error {
	auth := authn.FromConfig(authn.AuthConfig{
		Username: f.username,
		Password: f.password,
	})

	options := []crane.Option{crane.WithAuth(auth)}
	if config.UseUnsecure() {
		options = append(options, crane.Insecure)
	}

	img, err := crane.Pull(f.url, options...)
	if err != nil {
		return fmt.Errorf("failed to pull the state artifact: %v", err)
	}

	tarContent := new(bytes.Buffer)
	if err := crane.Export(img, tarContent); err != nil {
		return fmt.Errorf("failed to export the state artifact: %v", err)
	}

	tr := tar.NewReader(tarContent)
	var artifactsJSON []byte

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read the tar archive: %v", err)
		}

		if hdr.Name == "artifacts.json" {
			artifactsJSON, err = io.ReadAll(tr)
			if err != nil {
				return fmt.Errorf("failed to read the artifacts.json file: %v", err)
			}
			break
		}
	}
	if artifactsJSON == nil {
		return fmt.Errorf("artifacts.json not found in the state artifact")
	}
	err = json.Unmarshal(artifactsJSON, &state)
	if err != nil {
		return fmt.Errorf("failed to parse the artifacts.json file: %v", err)
	}
	return nil
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
