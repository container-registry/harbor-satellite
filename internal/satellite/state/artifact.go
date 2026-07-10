package state

type ArtifactReader interface {
	GetRepository() string
	GetTags() []string
	GetDigest() string
	GetType() string
	IsDeleted() bool
	SetRepository(repository string)
	SetName(name string)
	GetName() string
}

type Artifact struct {
	Repository string   `json:"repository,omitempty"`
	Tags       []string `json:"tag,omitempty"`
	Labels     []string `json:"labels"`
	Type       string   `json:"type,omitempty"`
	Digest     string   `json:"digest,omitempty"`
	Deleted    bool     `json:"deleted"`
	Name       string   `json:"name,omitempty"`
}

func NewArtifact(deleted bool, repository string, tags []string, digest, artifactType string) ArtifactReader {
	return &Artifact{
		Deleted:    deleted,
		Repository: repository,
		Tags:       tags,
		Digest:     digest,
		Type:       artifactType,
	}
}

func (a *Artifact) GetLabels() []string {
	return a.Labels
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

func (a *Artifact) SetRepository(repository string) {
	a.Repository = repository
}

func (a *Artifact) SetName(name string) {
	a.Name = name
}
