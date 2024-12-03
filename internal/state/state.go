package state

import (
	"fmt"
	"strings"
)

// Registry defines an interface for registry operations
type StateReader interface {
	// GetRegistryURL returns the URL of the registry after removing the "https://" or "http://" prefix if present and the trailing "/"
	GetRegistryURL() string
	// GetArtifacts returns the list of artifacts that needs to be pulled
	GetArtifacts() []ArtifactReader
	// GetArtifactByRepository takes in the repository name and returns the artifact associated with it
	GetArtifactByRepository(repo string) (ArtifactReader, error)
	// GetArtifactByName takes in the name of the artifact and returns the artifact associated with it
	GetArtifactByNameAndTag(name, tag string) ArtifactReader
	// SetArtifacts sets the artifacts in the state
	SetArtifacts(artifacts []ArtifactReader)
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
	registry = strings.TrimPrefix(registry, "https://")
	registry = strings.TrimPrefix(registry, "http://")
	registry = strings.TrimSuffix(registry, "/")
	return registry
}

func (a *State) GetArtifacts() []ArtifactReader {
	var artifacts_reader []ArtifactReader
	for i := range a.Artifacts {
		artifacts_reader = append(artifacts_reader, &a.Artifacts[i])
	}
	return artifacts_reader
}

func (a *State) GetArtifactByRepository(repo string) (ArtifactReader, error) {
	for i := range a.Artifacts {
		if a.Artifacts[i].GetRepository() == repo {
			return &a.Artifacts[i], nil
		}
	}
	return nil, fmt.Errorf("artifact not found in the list")
}

func (a *State) GetArtifactByNameAndTag(name, tag string) ArtifactReader {
	for i := range a.Artifacts {
		if a.Artifacts[i].GetName() == name {
			for _, t := range a.Artifacts[i].GetTags() {
				if t == tag {
					return &a.Artifacts[i]
				}
			}
		}
	}
	return nil
}

func (a *State) SetArtifacts(artifacts []ArtifactReader) {
	// Clear existing artifacts
	a.Artifacts = []Artifact{}

	// Set new artifacts
	a.Artifacts = make([]Artifact, len(artifacts))
	for i, artifact := range artifacts {
		a.Artifacts[i] = *artifact.(*Artifact)
	}
}
