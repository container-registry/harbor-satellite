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
	groupName            string
	stateArtifactName   string
	stateArtifactReader StateReader
}

type URLStateFetcher struct {
	baseStateFetcher
	url string
}

type FileStateArtifactFetcher struct {
	baseStateFetcher
	filePath string
}

func NewURLStateFetcher() StateFetcher {
	url := config.GetRemoteRegistryURL()
	url = utils.FormatRegistryURL(url)
	return &URLStateFetcher{
		baseStateFetcher: baseStateFetcher{
			groupName:            config.GetGroupName(),
			stateArtifactName:   config.GetStateArtifactName(),
			stateArtifactReader: NewState(),
		},
		url: url,
	}
}

func NewFileStateFetcher() StateFetcher {
	return &FileStateArtifactFetcher{
		baseStateFetcher: baseStateFetcher{
			groupName:            config.GetGroupName(),
			stateArtifactName:   config.GetStateArtifactName(),
			stateArtifactReader: NewState(),
		},
		filePath: config.GetInput(),
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
		Username: config.GetHarborUsername(),
		Password: config.GetHarborPassword(),
	})

	options := []crane.Option{crane.WithAuth(auth)}
	if config.UseUnsecure() {
		options = append(options, crane.Insecure)
	}

	sourceRegistry := utils.FormatRegistryURL(config.GetRemoteRegistryURL())
	tag := "latest"

	img, err := crane.Pull(fmt.Sprintf("%s/%s/%s:%s", sourceRegistry, f.groupName, f.stateArtifactName, tag), options...)
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
