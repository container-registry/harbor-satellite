package state

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"container-registry.com/harbor-satellite/internal/config"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// Registry defines an interface for registry operations
type StateReader interface {
	// GetRegistryURL returns the URL of the registry after removing the "https://" or "http://" prefix if present and the trailing "/"
	GetRegistryURL() string
	// GetRegistryType returns the list of artifacts that needs to be pulled
	GetArtifacts() []ArtifactReader
	// GetArtifactByRepository takes in the repository name and returns the artifact associated with it
	GetArtifactByRepository(repo string) (ArtifactReader, error)
	// Compare the state artifact with the new state artifact
	HasStateChanged(newState StateReader) bool
}

type State struct {
	Registry  string     `json:"registry"`
	Artifacts []Artifact `json:"artifacts"`
}

func NewState() StateReader {
	state := &State{}
	return state
}

func (a *State) GetRegistryURL() string {
	registry := a.Registry
	if len(registry) >= 8 && registry[:8] == "https://" {
		registry = registry[8:]
	} else if len(registry) >= 7 && registry[:7] == "http://" {
		registry = registry[7:]
	}
	if len(registry) > 0 && registry[len(registry)-1] == '/' {
		registry = registry[:len(registry)-1]
	}
	return registry
}

func (a *State) GetArtifacts() []ArtifactReader {
	var artifact_readers []ArtifactReader
	for _, artifact := range a.Artifacts {
		artifact_readers = append(artifact_readers, &artifact)
	}
	return artifact_readers
}

func (a *State) GetArtifactByRepository(repo string) (ArtifactReader, error) {
	for _, artifact := range a.Artifacts {
		if artifact.GetRepository() == repo {
			return &artifact, nil
		}
	}
	return &Artifact{}, fmt.Errorf("artifact not found in the list")
}

func (a *State) HasStateChanged(newState StateReader) bool {
	if a.GetRegistryURL() != newState.GetRegistryURL() {
		return true
	}
	artifacts := a.GetArtifacts()
	newArtifacts := newState.GetArtifacts()
	if len(artifacts) != len(newArtifacts) {
		return true
	}
	for i, artifact := range artifacts {
		if artifact.HasChanged(newArtifacts[i]) {
			return true
		}
	}
	return false
}

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
	if len(url) >= 8 && url[:8] == "https://" {
		url = url[8:]
	} else if len(url) >= 7 && url[:7] == "http://" {
		url = url[7:]
	}
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
	state_reader, err := FromJSON(content, f.state_artifact_reader.(*State))
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
			fmt.Printf("Contents of %s:\n", info.Name())
			fmt.Println(string(content))

			state_reader, err = FromJSON(content, f.state_artifact_reader.(*State))
			if err != nil {
				return fmt.Errorf("failed to parse the state artifact file: %v", err)
			}

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
func FromJSON(data []byte, reg *State) (StateReader, error) {

	if err := json.Unmarshal(data, &reg); err != nil {
		fmt.Print("Error in unmarshalling")
		return nil, err
	}
	fmt.Print(reg)
	// Validation
	if reg.Registry == "" {
		return nil, fmt.Errorf("registry URL is required")
	}
	return reg, nil
}
