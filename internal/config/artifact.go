package config

// ArtifactReader defines an interface for reading artifact data
type ArtifactReader interface {
	// GetRepository returns the repository of the artifact
	GetRepository() string
	// GetTag returns the tag of the artifact
	GetTag() string
	// GetHash returns the hash of the artifact
	GetHash() string
}

// Artifact represents an artifact object in the registry
type Artifact struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Hash       string `json:"hash"`
}

func NewArtifact() ArtifactReader {
	return &Artifact{}
}

func (a *Artifact) GetRepository() string {
	return a.Repository
}

func (a *Artifact) GetTag() string {
	return a.Tag
}

func (a *Artifact) GetHash() string {
	return a.Hash
}
