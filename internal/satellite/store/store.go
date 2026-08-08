package store

import (
	"context"
	"fmt"

	"github.com/container-registry/harbor-satellite/pkg/config"
)

// Store is the destination for replicated OCI content.
type Store interface {
	Replicate(ctx context.Context, artifacts []Artifact) error
	Delete(ctx context.Context, artifacts []Artifact) error
}

var (
	_ Store = (*OCIStore)(nil)
	_ Store = (*RegistryStore)(nil)
)

// Artifact identifies content in a repository.
type Artifact struct {
	Name       string `json:"name"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Digest     string `json:"digest"`
}

func (a Artifact) Reference() string {
	return fmt.Sprintf("%s/%s:%s", a.Repository, a.Name, a.Tag)
}

// RegistryOptions configures a remote registry endpoint.
type RegistryOptions struct {
	Reference string
	Username  string
	Password  string
	PlainHTTP bool
	TLS       config.TLSConfig
}
