package state

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"container-registry.com/harbor-satellite/internal/config"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
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
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current working directory: %v", err)
	}
	// Creating a file store in the current working directory will be deleted later after reading the state artifact
	fs, err := file.New(fmt.Sprintf("%s/state-artifact", cwd))
	if err != nil {
		return nil, fmt.Errorf("failed to create file store: %v", err)
	}
	defer fs.Close()

	ctx := context.Background()

	repo, err := remote.NewRepository(fmt.Sprintf("%s/%s/%s", f.url, f.group_name, f.state_artifact_name))
	if err != nil {
		return nil, fmt.Errorf("failed to create remote repository: %v", err)
	}

	// Setting up the authentication for the remote registry
	repo.Client = &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.NewCache(),
		Credential: auth.StaticCredential(
			f.url,
			auth.Credential{
				Username: config.GetHarborUsername(),
				Password: config.GetHarborPassword(),
			},
		),
	}
	// Copy from the remote repository to the file store
	tag := "latest"
	_, err = oras.Copy(ctx, repo, tag, fs, tag, oras.DefaultCopyOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to copy from remote repository to file store: %v", err)
	}
	stateArtifactDir := filepath.Join(cwd, "state-artifact")

	var state_reader StateReader
	// Find the state artifact file in the state-artifact directory that is created temporarily
	err = filepath.Walk(stateArtifactDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if filepath.Ext(info.Name()) == ".json" {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			state_reader, err = FromJSON(content, f.state_artifact_reader)
			if err != nil {
				return fmt.Errorf("failed to parse the state artifact file: %v", err)
			}
			return nil
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to read the state artifact file: %v", err)
	}
	// Clean up everything inside the state-artifact folder
	err = os.RemoveAll(stateArtifactDir)
	if err != nil {
		return nil, fmt.Errorf("failed to remove state-artifact directory: %v", err)
	}
	return state_reader, nil
}

// FromJSON parses the input JSON data into a StateArtifactReader
func FromJSON(data []byte, reg StateReader) (StateReader, error) {
	if err := json.Unmarshal(data, &reg); err != nil {
		fmt.Print("Error in unmarshalling")
		return nil, err
	}
	// Validation
	if reg.GetRegistryURL()== "" {
		return nil, fmt.Errorf("registry URL is required")
	}
	return reg, nil
}
