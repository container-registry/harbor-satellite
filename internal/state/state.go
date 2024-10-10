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
	// Compare the state artifact with the new state artifact
	HasStateChanged(newState StateReader) bool
	// RemoveAllArtifacts remove all the artifacts from the state which contains null tags and return the new state reader
	RemoveArtifactsWithNullTags(stateWithNullTagsArtifacts StateReader) StateReader
	// GetArtifactByName takes in the name of the artifact and returns the artifact associated with it
	GetArtifactByNameAndTag(name, tag string) ArtifactReader
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

func (a *State) RemoveArtifactsWithNullTags(stateWithNullTagsArtifacts StateReader) StateReader {
	var newArtifactsWithoutNullTags []Artifact
	for _, artifact := range a.Artifacts {
		if artifact.Tags != nil || len(artifact.Tags) != 0 {
			newArtifactsWithoutNullTags = append(newArtifactsWithoutNullTags, artifact)
		}
	}
	stateWithNullTagsArtifacts.(*State).Artifacts = newArtifactsWithoutNullTags
	return stateWithNullTagsArtifacts
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
