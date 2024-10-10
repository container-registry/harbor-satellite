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
	FetchStateArtifact() (StateReader, error)
}

type baseStateFetcher struct {
	group_name            string
	state_artifact_name   string
	state_artifact_reader StateReader
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
			group_name:            config.GetGroupName(),
			state_artifact_name:   config.GetStateArtifactName(),
			state_artifact_reader: NewState(),
		},
		url: url,
	}
}

func NewFileStateFetcher() StateFetcher {
	return &FileStateArtifactFetcher{
		baseStateFetcher: baseStateFetcher{
			group_name:            config.GetGroupName(),
			state_artifact_name:   config.GetStateArtifactName(),
			state_artifact_reader: NewState(),
		},
		filePath: config.GetInput(),
	}
}

func (f *FileStateArtifactFetcher) FetchStateArtifact() (StateReader, error) {
	content, err := os.ReadFile(f.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read the state artifact file: %v", err)
	}
	err = json.Unmarshal(content, &f.state_artifact_reader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse the state artifact file: %v", err)
	}
	return f.state_artifact_reader, nil
}

func (f *URLStateFetcher) FetchStateArtifact() (StateReader, error) {
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

	img, err := crane.Pull(fmt.Sprintf("%s/%s/%s:%s", sourceRegistry, f.group_name, f.state_artifact_name, tag), options...)
	if err != nil {
		return nil, fmt.Errorf("failed to pull the state artifact: %v", err)
	}

	tarContent := new(bytes.Buffer)
	if err := crane.Export(img, tarContent); err != nil {
		return nil, fmt.Errorf("failed to export the state artifact: %v", err)
	}

	tr := tar.NewReader(tarContent)
	var artifactsJSON []byte

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read the tar archive: %v", err)
		}

		if hdr.Name == "artifacts.json" {
			artifactsJSON, err = io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("failed to read the artifacts.json file: %v", err)
			}
			break
		}
	}

	if artifactsJSON == nil {
		return nil, fmt.Errorf("artifacts.json not found in the state artifact")
	}
	err = json.Unmarshal(artifactsJSON, &f.state_artifact_reader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse the artifacts.json file: %v", err)
	}
	
	state, err := ProcessState(&f.state_artifact_reader)
	if err != nil {
		return nil, fmt.Errorf("failed to process the state: %v", err)
	}
	return *state, nil
}

func ProcessState(state *StateReader) (*StateReader, error) {
	for _, artifact := range (*state).GetArtifacts() {
		repo, image, err := utils.GetRepositoryAndImageNameFromArtifact(artifact.GetRepository())
		if err != nil {
			fmt.Printf("Error in getting repository and image name: %v", err)
			return nil, err
		}
		artifact.SetRepository(repo)
		artifact.SetName(image)
	}
	return state, nil
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
