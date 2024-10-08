package state

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"container-registry.com/harbor-satellite/internal/config"
	"container-registry.com/harbor-satellite/internal/utils"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
)

type StateFetcher interface {
	// Fetches the state artifact from the registry
	FetchStateArtifact() (StateReader, error)
}

type URLStateFetcher struct {
	url                   string
	group_name            string
	state_artifact_name   string
	state_artifact_reader StateReader
}

func NewURLStateFetcher() StateFetcher {
	url := config.GetRemoteRegistryURL()
	// Trim the "https://" or "http://" prefix if present
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	state_artifact_reader := NewState()
	return &URLStateFetcher{
		url:                   url,
		group_name:            config.GetGroupName(),
		state_artifact_name:   config.GetStateArtifactName(),
		state_artifact_reader: state_artifact_reader,
	}
}

type FileStateArtifactFetcher struct {
	filePath              string
	group_name            string
	state_artifact_name   string
	state_artifact_reader StateReader
}

func NewFileStateFetcher() StateFetcher {
	filePath := config.GetInput()
	state_artifact_reader := NewState()
	return &FileStateArtifactFetcher{
		filePath:              filePath,
		group_name:            config.GetGroupName(),
		state_artifact_name:   config.GetStateArtifactName(),
		state_artifact_reader: state_artifact_reader,
	}
}

func (f *FileStateArtifactFetcher) FetchStateArtifact() (StateReader, error) {
	/// Read the state artifact file from the file path
	content, err := os.ReadFile(f.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read the state artifact file: %v", err)
	}
	state_reader, err := FromJSON(content, f.state_artifact_reader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse the state artifact file: %v", err)
	}
	return state_reader, nil
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

	sourceRegistry := utils.FormatRegistryUrl(config.GetRemoteRegistryURL())
	group := config.GetGroupName()
	stateArtifactName := config.GetStateArtifactName()
	var tag string = "latest"
	fmt.Printf("Pulling state artifact from %s/%s/%s:%s\n", sourceRegistry, group, stateArtifactName, tag)
	fmt.Printf("Auth: %v\n", auth)

	// pull the state artifact from the central registry
	img, err := crane.Pull(fmt.Sprintf("%s/%s/%s:%s", sourceRegistry, group, stateArtifactName, tag), options...)
	if err != nil {
		return nil, fmt.Errorf("failed to pull the state artifact: %v", err)
	}

	tarContent := new(bytes.Buffer)
	if err := crane.Export(img, tarContent); err != nil {
		return nil, fmt.Errorf("failed to export the state artifact: %v", err)
	}

	// parse the state artifact
	tr := tar.NewReader(tarContent)
	var artifactsJSON []byte

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break // End of tar archive
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read the tar archive: %v", err)
		}

		if hdr.Name == "artifacts.json" {
			// Found `artifacts.json`, read the content
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

	return f.state_artifact_reader, nil
}

// FromJSON parses the input JSON data into a StateArtifactReader
func FromJSON(data []byte, reg StateReader) (StateReader, error) {
	if err := json.Unmarshal(data, &reg); err != nil {
		fmt.Print("Error in unmarshalling")
		return nil, err
	}
	// Validation
	if reg.GetRegistryURL() == "" {
		return nil, fmt.Errorf("registry URL is required")
	}
	return reg, nil
}
