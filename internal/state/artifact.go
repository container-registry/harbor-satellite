package state

import (
	"reflect"
)

// ArtifactReader defines an interface for reading artifact data
type ArtifactReader interface {
	GetRepository() string
	GetTags() []string
	GetDigest() string
	GetType() string
	IsDeleted() bool
	HasChanged(newArtifact ArtifactReader) bool
	SetRepository(repository string)
	SetName(name string)
	GetName() string
}

// Artifact represents an artifact object in the registry
type Artifact struct {
	Deleted    bool     `json:"deleted,omitempty"`
	Repository string   `json:"repository,omitempty"`
	Tags       []string `json:"tag,omitempty"`
	Digest     string   `json:"digest,omitempty"`
	Type       string   `json:"type,omitempty"`
	Name       string   `json:"name,omitempty"`
}

// NewArtifact creates a new Artifact object
func NewArtifact(deleted bool, repository string, tags []string, digest, artifactType string) ArtifactReader {
	return &Artifact{
		Deleted:    deleted,
		Repository: repository,
		Tags:       tags,
		Digest:     digest,
		Type:       artifactType,
	}
}

func (a *Artifact) GetRepository() string {
	return a.Repository
}

func (a *Artifact) GetTags() []string {
	return a.Tags
}

func (a *Artifact) GetDigest() string {
	return a.Digest
}

func (a *Artifact) GetType() string {
	return a.Type
}

func (a *Artifact) IsDeleted() bool {
	return a.Deleted
}

func (a *Artifact) GetName() string {
	return a.Name
}

// HasChanged compares the current artifact with another to determine if there are any changes
func (a *Artifact) HasChanged(newArtifact ArtifactReader) bool {
	// Compare the digest (hash)
	if a.GetDigest() != newArtifact.GetDigest() {
		return true
	}

	// Compare the repository
	if a.GetRepository() != newArtifact.GetRepository() {
		return true
	}

	// Compare the tags (order-agnostic comparison)
	if !reflect.DeepEqual(a.GetTags(), newArtifact.GetTags()) {
		return true
	}

	// Compare the deletion status
	if a.IsDeleted() != newArtifact.IsDeleted() {
		return true
	}

	if a.GetType() != newArtifact.GetType() {
		return true
	}

	// Compare the tags (order-agnostic comparison using reflect.DeepEqual)
	if !reflect.DeepEqual(a.GetTags(), newArtifact.GetTags()) {
		return true
	}

	// No changes detected
	return false
}

func (a *Artifact) SetRepository(repository string) {
	a.Repository = repository
}

func (a *Artifact) SetName(name string) {
	a.Name = name
}
