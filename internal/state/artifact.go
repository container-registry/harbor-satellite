package state

// ArtifactReader defines an interface for reading artifact data
type ArtifactReader interface {
	// GetRepository returns the repository of the artifact
	GetRepository() string
	// GetTag returns the tag of the artifact
	GetTag() string
	// GetHash returns the hash of the artifact
	GetHash() string
	// HasChanged returns true if the artifact has changed
	HasChanged(newArtifact ArtifactReader) bool
}

// Artifact represents an artifact object in the registry
type Artifact struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Hash       string `json:"hash"`
}

func NewArtifact(repository, tag, hash string) ArtifactReader {
	return &Artifact{
		Repository: repository,
		Tag:        tag,
		Hash:       hash,
	}
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

func (a *Artifact) HasChanged(newArtifact ArtifactReader) bool {
	return a.GetHash() != newArtifact.GetHash()
}
