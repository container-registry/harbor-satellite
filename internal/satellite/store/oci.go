package store

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/logger"
	satTLS "github.com/container-registry/harbor-satellite/internal/satellite/tls"
	"github.com/container-registry/harbor-satellite/pkg/config"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// OCIStore persists replicated content in a single OCI image layout.
type OCIStore struct {
	source RegistryOptions
	target *oci.Store
	mu     sync.Mutex
}

// NewOCIStore opens or creates an OCI image-layout store at root and configures
// the remote registry used as its replication source.
func NewOCIStore(root string, source RegistryOptions) (*OCIStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("OCI store root is required")
	}

	target, err := oci.New(root)
	if err != nil {
		return nil, fmt.Errorf("open OCI store at %s: %w", root, err)
	}

	return &OCIStore{source: source, target: target}, nil
}

// Replicate copies complete OCI descriptor graphs from a remote repository to
// the local OCI layout. High-level operations are serialized so index updates
// and garbage collection cannot interleave with an in-progress graph copy.
func (s *OCIStore) Replicate(ctx context.Context, artifacts []Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	log := logger.FromContext(ctx)
	for _, artifact := range artifacts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := artifact.validate(); err != nil {
			return err
		}

		source, err := newRepository(s.source, artifact)
		if err != nil {
			return err
		}
		destinationRef := s.reference(artifact)

		sourceIdentifier := artifact.sourceIdentifier()
		desc, err := source.Resolve(ctx, sourceIdentifier)
		if err != nil {
			return fmt.Errorf("resolve source artifact %s: %w", destinationRef, err)
		}
		current, err := s.target.Resolve(ctx, destinationRef)
		if err == nil && current.Digest == desc.Digest {
			log.Info().Str("reference", destinationRef).Msg("Artifact already up-to-date in OCI store, skipping")
			continue
		}
		if err != nil && !errors.Is(err, errdef.ErrNotFound) {
			return fmt.Errorf("resolve OCI store reference %s: %w", destinationRef, err)
		}

		if _, err := oras.Copy(ctx, source, sourceIdentifier, s.target, destinationRef, oras.DefaultCopyOptions); err != nil {
			return fmt.Errorf("copy artifact %s to OCI store: %w", destinationRef, err)
		}
		log.Info().Str("reference", destinationRef).Str("digest", desc.Digest.String()).Msg("Artifact replicated to OCI store")
	}

	return nil
}

// Delete removes the selected references and garbage-collects content that is
// no longer reachable from another retained reference.
func (s *OCIStore) Delete(ctx context.Context, artifacts []Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	for _, artifact := range artifacts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := artifact.validate(); err != nil {
			return err
		}

		reference := s.reference(artifact)
		if _, err := s.target.Resolve(ctx, reference); err != nil {
			if errors.Is(err, errdef.ErrNotFound) {
				continue
			}
			return fmt.Errorf("resolve OCI store reference %s: %w", reference, err)
		}
		if err := s.target.Untag(ctx, reference); err != nil {
			return fmt.Errorf("untag OCI store reference %s: %w", reference, err)
		}
		changed = true
	}

	if changed {
		if err := s.target.GC(ctx); err != nil {
			return fmt.Errorf("garbage collect OCI store: %w", err)
		}
	}

	return nil
}

// reference retains source provenance and prevents equal repository/tag names
// from different registries from colliding in the shared OCI layout.
func (s *OCIStore) reference(artifact Artifact) string {
	return s.source.reference(artifact, artifact.destinationIdentifier())
}

// newRepository creates an authenticated ORAS repository for one source
// artifact, applying the endpoint repository override when configured.
func newRepository(options RegistryOptions, artifact Artifact) (*remote.Repository, error) {
	registry := normalizeRegistry(options.Endpoint)
	repository, err := remote.NewRepository(strings.Join([]string{
		strings.TrimSuffix(registry, "/"),
		options.repositoryPath(artifact),
	}, "/"))
	if err != nil {
		return nil, fmt.Errorf("create remote repository for %s: %w", artifact.Reference(), err)
	}
	repository.PlainHTTP = options.PlainHTTP

	host, err := registryHost(registry)
	if err != nil {
		return nil, err
	}
	client, err := registryHTTPClient(options.TLS)
	if err != nil {
		return nil, err
	}
	repository.Client = &auth.Client{
		Client: client,
		Cache:  auth.NewCache(),
		Credential: auth.StaticCredential(host, auth.Credential{
			Username: options.Username,
			Password: options.Password,
		}),
	}

	return repository, nil
}

// normalizeRegistry removes an optional scheme and trailing slash so registry
// references can be consumed consistently by ORAS and go-containerregistry.
func normalizeRegistry(reference string) string {
	reference = strings.TrimSpace(reference)
	reference = strings.TrimPrefix(reference, "https://")
	reference = strings.TrimPrefix(reference, "http://")
	return strings.TrimSuffix(reference, "/")
}

// registryHost extracts the host used to scope registry credentials.
func registryHost(reference string) (string, error) {
	parsed, err := url.Parse("//" + reference)
	if err != nil {
		return "", fmt.Errorf("parse registry reference %q: %w", reference, err)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("registry reference %q has no host", reference)
	}
	return parsed.Host, nil
}

// registryHTTPClient returns the default retrying ORAS client unless custom TLS
// settings require a cloned transport.
func registryHTTPClient(cfg config.TLSConfig) (*http.Client, error) {
	if cfg.CertFile == "" && cfg.KeyFile == "" && cfg.CAFile == "" && !cfg.SkipVerify {
		return retry.DefaultClient, nil
	}

	tlsConfig, err := satTLS.LoadClientTLSConfig(&satTLS.Config{
		CertFile:   cfg.CertFile,
		KeyFile:    cfg.KeyFile,
		CAFile:     cfg.CAFile,
		SkipVerify: cfg.SkipVerify,
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return nil, fmt.Errorf("load registry TLS config: %w", err)
	}

	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("default HTTP transport is not an *http.Transport")
	}
	transport := defaultTransport.Clone()
	transport.TLSClientConfig = tlsConfig
	client := *retry.DefaultClient
	client.Transport = transport
	return &client, nil
}
