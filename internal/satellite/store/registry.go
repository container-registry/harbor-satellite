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

	satTLS "github.com/container-registry/harbor-satellite/internal/satellite/tls"
	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/container-registry/harbor-satellite/pkg/config"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// RegistryStore provides direct ORAS access to one OCI registry. The option
// source may change after registration or a configuration reload.
type RegistryStore struct {
	options func() (RegistryOptions, error)
	mu      sync.Mutex
	current RegistryOptions
	client  *auth.Client
	// verifier checks signatures before Replicate copies a root.
	verifier SignatureVerifier
}

// SetSignatureVerifier makes Replicate check signatures before it copies a
// root. Call it before the store is used.
func (r *RegistryStore) SetSignatureVerifier(verifier SignatureVerifier) {
	r.verifier = verifier
}

func NewRegistryStore(options RegistryOptions) (*RegistryStore, error) {
	if err := options.validate(); err != nil {
		return nil, err
	}
	return &RegistryStore{options: func() (RegistryOptions, error) { return options, nil }}, nil
}

// NewRegistryStoreWithOptions resolves registry settings for each operation.
// This lets a shared source store observe refreshed credentials without
// changing readers or graph copies already in progress.
func NewRegistryStoreWithOptions(options func() (RegistryOptions, error)) (*RegistryStore, error) {
	if options == nil {
		return nil, errors.New("registry options source is required")
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

func (r *RegistryStore) Fetch(ctx context.Context, artifact Artifact, descriptor ocispec.Descriptor, resource PullResource, headers http.Header) (*http.Response, error) {
	repository, err := r.repository(artifact)
	if err != nil {
		return nil, err
	}
	path := "blobs"
	if resource == PullResourceManifest {
		path = "manifests"
	}
	scheme := "https"
	if repository.PlainHTTP {
		scheme = "http"
	}
	endpoint := url.URL{
		Scheme: scheme,
		Host:   repository.Reference.Host(),
		Path:   "/v2/" + repository.Reference.Repository + "/" + path + "/" + descriptor.Digest.String(),
	}
	ctx = auth.AppendRepositoryScope(ctx, repository.Reference, auth.ActionPull)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	// Forward representation controls, never client credentials or hop-by-hop
	// headers. The shared ORAS client supplies scoped registry authentication.
	for _, name := range []string{"Range", "If-Range", "If-Match", "If-None-Match", "If-Modified-Since", "If-Unmodified-Since"} {
		for _, value := range headers.Values(name) {
			request.Header.Add(name, value)
		}
	}
	request.Header.Set("Accept-Encoding", "identity")
	if descriptor.MediaType != "" {
		request.Header.Set("Accept", descriptor.MediaType)
	}
	response, err := repository.Client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusNotFound {
		_ = response.Body.Close()
		return nil, errdef.ErrNotFound
	}
	if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusPartialContent {
		if value := response.Header.Get("Docker-Content-Digest"); value != "" && value != descriptor.Digest.String() {
			_ = response.Body.Close()
			return nil, errors.New("registry content digest does not match resolved descriptor")
		}
		if response.StatusCode == http.StatusOK && response.ContentLength >= 0 && descriptor.Size >= 0 && response.ContentLength != descriptor.Size {
			_ = response.Body.Close()
			return nil, errors.New("registry content length does not match resolved descriptor")
		}
		response.Header.Set("Docker-Content-Digest", descriptor.Digest.String())
	}
	return response, nil
}

func (r *RegistryStore) Replicate(ctx context.Context, source Store, artifacts []Artifact) error {
	sourceTarget, ok := source.(storeTarget)
	if !ok {
		return errors.New("source store does not support OCI graph transfer")
	}
	var rejected []error
	for _, artifact := range artifacts {
		err := r.replicateOne(ctx, source, sourceTarget, artifact)
		if errors.Is(err, ErrSignatureRejected) {
			rejected = append(rejected, err)
			continue
		}
		if err != nil {
			return err
		}
	}
	return errors.Join(rejected...)
}

func (r *RegistryStore) replicateOne(ctx context.Context, source Store, sourceTarget storeTarget, artifact Artifact) error {
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
		return oras.CopyGraph(ctx, from, to, desc, oras.DefaultCopyGraphOptions)
	}
	// oras.Copy tags inside the transfer, so copy the graph by descriptor
	// and tag it only after the root matches the group list. Look the root
	// up by the listed digest: a source tag can still name an older root.
	lookup := artifact
	if artifact.Digest != "" {
		lookup.Tag = ""
	}
	desc, err := source.Pull(ctx, lookup, PullResourceManifest)
	if err != nil {
		return err
	}
	if err := rejectUnlistedDigest(desc, artifact); err != nil {
		return err
	}
	chain, err := discoverSupplyChain(ctx, from, from, desc)
	if err != nil {
		return fmt.Errorf("find signatures of %s: %w", artifact.Reference(), err)
	}
	if err := verifySignatures(ctx, r.verifier, artifact, chain, from, desc); err != nil {
		return err
	}
	if err := oras.CopyGraph(ctx, from, to, desc, oras.DefaultCopyGraphOptions); err != nil {
		return err
	}
	// Signatures go in before the root tag, so a tagged root always has
	// the signatures it had at the source.
	if err := copySupplyChainToRegistry(ctx, chain, from, to, desc); err != nil {
		return fmt.Errorf("copy signatures of %s: %w", artifact.Reference(), err)
	}
	if err := to.Tag(ctx, desc, artifact.destinationIdentifier()); err != nil {
		return err
	}
	return nil
}

// copySupplyChainToRegistry copies the signatures and referrers of root. A
// cosign tag keeps its source name. A referrer is pushed with its subject, so
// ORAS updates the destination's referrers tag schema index when the
// destination has no referrers API.
func copySupplyChainToRegistry(ctx context.Context, chain supplyChain, from oras.ReadOnlyTarget, to *remote.Repository, root ocispec.Descriptor) error {
	if chain.empty() {
		return nil
	}
	if err := chain.copyTo(ctx, from, to); err != nil {
		return err
	}
	for _, tagged := range chain.tagged {
		if err := to.Tag(ctx, tagged.desc, tagged.tag); err != nil {
			return fmt.Errorf("tag %s: %w", tagged.tag, err)
		}
	}
	logger.FromContext(ctx).Info().Str("digest", root.Digest.String()).
		Int("signature_manifests", chain.manifestCount()).Msg("signatures copied to registry")
	return nil
}

// Referrers lists the referrers of subject through the registry API, or the
// referrers tag schema when the registry has no referrers API.
func (r *RegistryStore) Referrers(ctx context.Context, artifact Artifact, subject digest.Digest, artifactType string) ([]ocispec.Descriptor, error) {
	if err := subject.Validate(); err != nil {
		return nil, fmt.Errorf("referrers subject: %w", err)
	}
	repository, err := r.repository(artifact)
	if err != nil {
		return nil, err
	}
	// The subject manifest need not exist: referrers can outlive it or
	// arrive first, and both APIs are keyed by digest alone.
	var referrers []ocispec.Descriptor
	err = repository.Referrers(ctx, ocispec.Descriptor{Digest: subject}, artifactType, func(page []ocispec.Descriptor) error {
		referrers = append(referrers, page...)
		return nil
	})
	if errors.Is(err, errdef.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// The tag schema fallback returns every referrer. Filter again so both
	// paths honour artifactType.
	return filterArtifactType(referrers, artifactType), nil
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
	options, err := r.options()
	if err != nil {
		return nil, err
	}
	if err := options.validate(); err != nil {
		return nil, err
	}

	r.mu.Lock()
	if r.client == nil || r.current != options {
		client, err := newRegistryAuthClient(options)
		if err != nil {
			r.mu.Unlock()
			return nil, err
		}
		r.client = client
		r.current = options
	}
	client := r.client
	r.mu.Unlock()
	return newRepository(options, artifact, client)
}

type storeTarget interface {
	targetFor(Artifact) (oras.Target, error)
}

func newRepository(options RegistryOptions, artifact Artifact, client *auth.Client) (*remote.Repository, error) {
	registry := normalizeRegistry(options.Endpoint)
	repository, err := remote.NewRepository(strings.TrimSuffix(registry, "/") + "/" + options.repositoryPath(artifact))
	if err != nil {
		return nil, fmt.Errorf("create remote repository: %w", err)
	}
	repository.PlainHTTP = options.PlainHTTP
	repository.Client = client
	return repository, nil
}

func newRegistryAuthClient(options RegistryOptions) (*auth.Client, error) {
	registry := normalizeRegistry(options.Endpoint)
	host, err := registryHost(registry)
	if err != nil {
		return nil, err
	}
	client, err := registryHTTPClient(options.TLS)
	if err != nil {
		return nil, err
	}
	return &auth.Client{
		Client:     client,
		Cache:      auth.NewCache(),
		Credential: auth.StaticCredential(host, auth.Credential{Username: options.Username, Password: options.Password}),
	}, nil
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
