package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/container-registry/harbor-satellite/pkg/config"
)

// Store abstracts a destination that can replicate and remove OCI artifacts.
// Implementations may persist content locally or copy it to a remote registry.
type Store interface {
	// Replicate copies the supplied artifacts from the configured source into
	// the store.
	Replicate(ctx context.Context, artifacts []Artifact) error
	// Delete removes the supplied artifact references from the store.
	Delete(ctx context.Context, artifacts []Artifact) error
}

var (
	_ Store = (*OCIStore)(nil)
	_ Store = (*RegistryStore)(nil)
)

// Artifact identifies OCI content independently of its source or destination
// registry. Repository is retained as a fallback for state payloads that carry
// their namespace per artifact; an endpoint-level repository takes precedence.
type Artifact struct {
	// Name is the required image or artifact name within its repository.
	Name string `json:"name"`
	// Repository is the namespace fallback used when RegistryOptions.Repository
	// is empty.
	Repository string `json:"repository"`
	// Tag is the mutable identifier applied at the destination.
	Tag string `json:"tag"`
	// Digest is the preferred immutable source identifier when present.
	Digest string `json:"digest"`
}

// Reference returns the artifact's repository-qualified destination reference.
// It uses the tag when available and otherwise uses the digest.
func (a Artifact) Reference() string {
	return artifactReference(a.Repository, a.Name, a.destinationIdentifier())
}

// sourceIdentifier returns the immutable digest when one is available and
// otherwise returns the tag. Full name@digest input is reduced to the digest.
func (a Artifact) sourceIdentifier() string {
	if a.Digest == "" {
		return a.Tag
	}
	if _, digest, found := strings.Cut(a.Digest, "@"); found {
		return digest
	}
	return a.Digest
}

// destinationIdentifier returns the tag used to name copied content, falling
// back to the digest for digest-only artifacts.
func (a Artifact) destinationIdentifier() string {
	if a.Tag != "" {
		return a.Tag
	}
	return a.sourceIdentifier()
}

// validate checks that the artifact has the minimum information required for
// either a remote or local store operation.
func (a Artifact) validate() error {
	if strings.Trim(a.Name, "/ ") == "" {
		return errors.New("artifact image name is required")
	}
	if a.sourceIdentifier() == "" {
		return errors.New("artifact tag or digest is required")
	}
	return nil
}

// RegistryOptions configures one remote registry endpoint and its credentials.
type RegistryOptions struct {
	// Endpoint is the registry host, optionally including an HTTP(S) scheme.
	Endpoint string
	// Repository is an optional namespace prefix. When empty, the repository
	// supplied by each Artifact is used for backward compatibility.
	Repository string
	// Username is the registry account used for authentication.
	Username string
	// Password is the registry secret used for authentication.
	Password string
	// PlainHTTP permits unencrypted registry communication.
	PlainHTTP bool
	// TLS configures certificate validation for HTTPS registry communication.
	TLS config.TLSConfig
}

// repositoryPath returns the endpoint-specific repository path for an artifact.
// The configured endpoint repository overrides the artifact fallback.
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

// reference returns a complete registry reference for an artifact and tag or
// digest identifier.
func (o RegistryOptions) reference(artifact Artifact, identifier string) string {
	return strings.TrimSuffix(normalizeRegistry(o.Endpoint), "/") + "/" +
		artifactReference(o.repositoryPath(artifact), "", identifier)
}

// artifactReference joins a repository, image name, and tag or digest using
// the separator required by the identifier type.
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
