package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// Registry defines an interface for registry operations
type StateReader interface {
	// GetRegistryURL returns the URL of the registry
	GetRegistryURL() string
	// GetRegistryType returns the list of artifacts that needs to be pulled
	GetArtifacts() []ArtifactReader
	// GetArtifactByRepository takes in the repository name and returns the artifact associated with it
	GetArtifactByRepository(repo string) (ArtifactReader, error)
}

type State struct {
	Registry  string     `json:"registry"`
	Artifacts []Artifact `json:"artifacts"`
}

func NewState(artifact *Artifact) StateReader {
	state := &State{
		Registry:  "",
		Artifacts: []Artifact{*artifact},
	}
	return state
}

func (a *State) GetRegistryURL() string {
	return a.Registry
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

type StateArtifactFetcher interface {
	// Fetches the state artifact from the registry
	FetchStateArtifact() error
}

type URLStateArtifactFetcher struct {
	url                   string
	group_name            string
	state_artifact_name   string
	state_artifact_reader StateReader
}

func NewURLStateArtifactFetcher() StateArtifactFetcher {
	url := GetRemoteRegistryURL()
	// Trim the "https://" or "http://" prefix if present
	if len(url) >= 8 && url[:8] == "https://" {
		url = url[8:]
	} else if len(url) >= 7 && url[:7] == "http://" {
		url = url[7:]
	}
	artifact := NewArtifact()
	state_artifact_reader := NewState(artifact.(*Artifact))
	return &URLStateArtifactFetcher{
		url:                   url,
		group_name:            GetGroupName(),
		state_artifact_name:   GetStateArtifactName(),
		state_artifact_reader: state_artifact_reader,
	}
}

type FileStateArtifactFetcher struct {
	filePath string
}

func (f *URLStateArtifactFetcher) FetchStateArtifact() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %v", err)
	}
	// Creating a file store in the current working directory
	fs, err := file.New(fmt.Sprintf("%s/state-artifact", cwd))
	if err != nil {
		return fmt.Errorf("failed to create file store: %v", err)
	}
	defer fs.Close()

	ctx := context.Background()

	repo, err := remote.NewRepository(fmt.Sprintf("%s/%s/%s", f.url, f.group_name, f.state_artifact_name))
	if err != nil {
		return fmt.Errorf("failed to create remote repository: %v", err)
	}

	// Setting up the authentication for the remote registry
	repo.Client = &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.NewCache(),
		Credential: auth.StaticCredential(
			f.url,
			auth.Credential{
				Username: GetHarborUsername(),
				Password: GetHarborPassword(),
			},
		),
	}
	// Copy from the remote repository to the file store
	tag := "latest"
	_, err = oras.Copy(ctx, repo, tag, fs, tag, oras.DefaultCopyOptions)
	if err != nil {
		return fmt.Errorf("failed to copy from remote repository to file store: %v", err)
	}
	stateArtifactDir := filepath.Join(cwd, "state-artifact")
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

			state_artifact_reader, err := FromJSON(content, f.state_artifact_reader.(*State))
			if err != nil {
				return fmt.Errorf("failed to parse the state artifact file: %v", err)
			}
			fmt.Println(state_artifact_reader)

		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to read the state artifact file: %v", err)
	}
	// Clean up everything inside the state-artifact folder
	err = os.RemoveAll(stateArtifactDir)
	if err != nil {
		return fmt.Errorf("failed to remove state-artifact directory: %v", err)
	}
	return nil
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
