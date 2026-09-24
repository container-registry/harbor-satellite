package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/container-registry/harbor-satellite/pkg/config"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Store is a location where Satellite can resolve, stream, replicate, and
// remove OCI content. Pull accepts metadata already parsed by the proxy and
// returns only immutable OCI descriptor data; Fetch owns the response stream.
type Store interface {
	Pull(ctx context.Context, artifact Artifact, resource PullResource) (ocispec.Descriptor, error)
	Fetch(ctx context.Context, artifact Artifact, descriptor ocispec.Descriptor) (io.ReadCloser, error)
	Replicate(ctx context.Context, source Store, artifacts []Artifact) error
	Delete(ctx context.Context, artifacts []Artifact) error
}

var (
	_ Store = (*OCIStore)(nil)
	_ Store = (*RegistryStore)(nil)
)

// PullResource identifies the OCI Distribution resource addressed by a pull.
// It is intentionally small: route parsing belongs to the proxy package.
type PullResource uint8

const (
	PullResourceManifest PullResource = iota + 1
	PullResourceBlob
)

// Artifact identifies OCI content independently of a store implementation.
// A proxy manifest pull supplies Name and Tag (a tag or manifest digest). A
// proxy blob pull supplies Name and Digest. State replication uses Repository
// and Name as before.
type Artifact struct {
	Name       string `json:"name"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Digest     string `json:"digest"`
}

// Reference returns the repository-qualified local reference. It uses Tag
// when available and otherwise uses Digest.
func (a Artifact) Reference() string {
	return artifactReference(a.Repository, a.Name, a.destinationIdentifier())
}

func (a Artifact) sourceIdentifier() string {
	if a.Digest == "" {
		return a.Tag
	}
	if _, digest, found := strings.Cut(a.Digest, "@"); found {
		return digest
	}
	return a.Digest
}

func (a Artifact) destinationIdentifier() string {
	if a.Tag != "" {
		return a.Tag
	}
	return a.sourceIdentifier()
}

// validate is used by state replication and deletion. Pull request validation
// is deliberately owned by the proxy image process.
func (a Artifact) validate() error {
	if strings.Trim(a.Name, "/ ") == "" {
		return errors.New("artifact image name is required")
	}
	if a.sourceIdentifier() == "" {
		return errors.New("artifact tag or digest is required")
	}
	return nil
}

// RegistryOptions configures a registry-backed store.
type RegistryOptions struct {
	Endpoint   string
	Repository string
	Username   string
	Password   string
	PlainHTTP  bool
	TLS        config.TLSConfig
}

func (o RegistryOptions) validate() error {
	if normalizeRegistry(o.Endpoint) == "" {
		return errors.New("registry endpoint is required")
	}
	_, err := registryHost(normalizeRegistry(o.Endpoint))
	return err
}

// repositoryPath returns the endpoint-specific repository path for an artifact.
func (o RegistryOptions) repositoryPath(artifact Artifact) string {
	repository := o.Repository
	if repository == "" {
		repository = artifact.Repository
	}
	return strings.Trim(strings.Join([]string{
		strings.Trim(repository, "/"),
		strings.Trim(artifact.Name, "/"),
	}, "/"), "/")
}

func (o RegistryOptions) reference(artifact Artifact, identifier string) string {
	return strings.TrimSuffix(normalizeRegistry(o.Endpoint), "/") + "/" +
		artifactReference(o.repositoryPath(artifact), "", identifier)
}

func artifactReference(repository, name, identifier string) string {
	path := strings.Trim(strings.Join([]string{
		strings.Trim(repository, "/"),
		strings.Trim(name, "/"),
	}, "/"), "/")
	separator := ":"
	if strings.Contains(identifier, ":") {
		separator = "@"
	}
	return fmt.Sprintf("%s%s%s", path, separator, identifier)
}
