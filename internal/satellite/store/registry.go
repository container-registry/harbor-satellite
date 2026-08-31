package store

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	satTLS "github.com/container-registry/harbor-satellite/internal/satellite/tls"
	"github.com/container-registry/harbor-satellite/pkg/config"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// RegistryStore provides direct ORAS access to one OCI registry.
type RegistryStore struct{ options RegistryOptions }

func NewRegistryStore(options RegistryOptions) (*RegistryStore, error) {
	if err := options.validate(); err != nil {
		return nil, err
	}
	return &RegistryStore{options: options}, nil
}

func (r *RegistryStore) Pull(ctx context.Context, artifact Artifact, resource PullResource) (ocispec.Descriptor, error) {
	repository, err := r.repository(artifact)
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	switch resource {
	case PullResourceManifest:
		return repository.Manifests().Resolve(ctx, artifact.sourceIdentifier())
	case PullResourceBlob:
		return repository.Blobs().Resolve(ctx, artifact.sourceIdentifier())
	default:
		return ocispec.Descriptor{}, errdef.ErrNotFound
	}
}

func (r *RegistryStore) Fetch(ctx context.Context, artifact Artifact, descriptor ocispec.Descriptor) (io.ReadCloser, error) {
	repository, err := r.repository(artifact)
	if err != nil {
		return nil, err
	}
	return repository.Fetch(ctx, descriptor)
}

func (r *RegistryStore) Replicate(ctx context.Context, source Store, artifacts []Artifact) error {
	sourceTarget, ok := source.(storeTarget)
	if !ok {
		return errors.New("source store does not support OCI graph transfer")
	}
	for _, artifact := range artifacts {
		if err := artifact.validate(); err != nil {
			return err
		}
		from, err := sourceTarget.targetFor(artifact)
		if err != nil {
			return err
		}
		to, err := r.repository(artifact)
		if err != nil {
			return err
		}
		if artifact.Tag == "" {
			desc, err := source.Pull(ctx, artifact, PullResourceBlob)
			if err != nil {
				return err
			}
			if err := oras.CopyGraph(ctx, from, to, desc, oras.DefaultCopyGraphOptions); err != nil {
				return err
			}
			continue
		}
		if _, err := oras.Copy(ctx, from, artifact.sourceIdentifier(), to, artifact.destinationIdentifier(), oras.DefaultCopyOptions); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes manifest roots from this registry.
func (r *RegistryStore) Delete(ctx context.Context, artifacts []Artifact) error {
	for _, artifact := range artifacts {
		if err := artifact.validate(); err != nil {
			return err
		}
		repository, err := r.repository(artifact)
		if err != nil {
			return err
		}
		desc, err := repository.Manifests().Resolve(ctx, artifact.destinationIdentifier())
		if errors.Is(err, errdef.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if err := repository.Delete(ctx, desc); err != nil {
			return err
		}
	}
	return nil
}

func (r *RegistryStore) targetFor(artifact Artifact) (oras.Target, error) {
	return r.repository(artifact)
}

func (r *RegistryStore) repository(artifact Artifact) (*remote.Repository, error) {
	return newRepository(r.options, artifact)
}

type storeTarget interface {
	targetFor(Artifact) (oras.Target, error)
}

func newRepository(options RegistryOptions, artifact Artifact) (*remote.Repository, error) {
	registry := normalizeRegistry(options.Endpoint)
	repository, err := remote.NewRepository(strings.TrimSuffix(registry, "/") + "/" + options.repositoryPath(artifact))
	if err != nil {
		return nil, fmt.Errorf("create remote repository: %w", err)
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
		Client:     client,
		Cache:      auth.NewCache(),
		Credential: auth.StaticCredential(host, auth.Credential{Username: options.Username, Password: options.Password}),
	}
	return repository, nil
}

func normalizeRegistry(reference string) string {
	reference = strings.TrimSpace(reference)
	reference = strings.TrimPrefix(reference, "https://")
	reference = strings.TrimPrefix(reference, "http://")
	return strings.TrimSuffix(reference, "/")
}

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

func registryHTTPClient(cfg config.TLSConfig) (*http.Client, error) {
	if cfg.CertFile == "" && cfg.KeyFile == "" && cfg.CAFile == "" && !cfg.SkipVerify {
		return retry.DefaultClient, nil
	}
	tlsConfig, err := satTLS.LoadClientTLSConfig(&satTLS.Config{
		CertFile: cfg.CertFile, KeyFile: cfg.KeyFile, CAFile: cfg.CAFile, SkipVerify: cfg.SkipVerify, MinVersion: tls.VersionTLS12,
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
