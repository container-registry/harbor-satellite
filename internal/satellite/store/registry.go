package store

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"sync"

	satTLS "github.com/container-registry/harbor-satellite/internal/satellite/tls"
	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// RegistryStore copies OCI images between remote registries using Crane.
type RegistryStore struct {
	source      RegistryOptions
	destination RegistryOptions
}

// NewRegistryStore creates a store that copies OCI images from one remote
// registry endpoint to another.
func NewRegistryStore(source, destination RegistryOptions) Store {
	return &RegistryStore{
		source:      source,
		destination: destination,
	}
}

// Replicate copies images from the source registry to the destination registry.
// Before pulling, it checks which blobs already exist at the destination and
// only downloads missing layers from source, saving bandwidth on crash recovery.
// Entities are dispatched to a bounded pool of goroutines for concurrent replication.
func (r *RegistryStore) Replicate(ctx context.Context, replicationEntities []Artifact) error {
	pullAuth := authn.FromConfig(authn.AuthConfig{
		Username: r.source.Username,
		Password: r.source.Password,
	})
	pushAuth := authn.FromConfig(authn.AuthConfig{
		Username: r.destination.Username,
		Password: r.destination.Password,
	})

	var nameOpts []name.Option
	pullOpts := []remote.Option{remote.WithAuth(pullAuth), remote.WithContext(ctx)}
	pushOpts := []remote.Option{remote.WithAuth(pushAuth), remote.WithContext(ctx)}

	if r.source.PlainHTTP {
		nameOpts = append(nameOpts, name.Insecure)
	} else {
		transport, err := r.buildTLSTransport()
		if err != nil {
			return fmt.Errorf("build TLS transport: %w", err)
		}
		if transport != nil {
			pullOpts = append(pullOpts, remote.WithTransport(transport))
			pushOpts = append(pushOpts, remote.WithTransport(transport))
		}
	}

	const maxWorkers = 5
	workers := min(maxWorkers, len(replicationEntities))
	if workers == 0 {
		return nil
	}

	entityCh := make(chan Artifact)
	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for entity := range entityCh {
				if err := r.replicateEntity(ctx, entity, nameOpts, pullOpts, pushOpts); err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
			}
		}()
	}

dispatch:
	for _, entity := range replicationEntities {
		select {
		case <-ctx.Done():
			break dispatch
		case entityCh <- entity:
		}
	}
	close(entityCh)
	wg.Wait()

	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.Join(errs...)
}

func (r *RegistryStore) replicateEntity(ctx context.Context, entity Artifact, nameOpts []name.Option, pullOpts, pushOpts []remote.Option) error {
	log := logger.FromContext(ctx)

	if err := entity.validate(); err != nil {
		log.Warn().Err(err).Str("entity", entity.Name).Msg("Skipping entity: validation failed")
		return fmt.Errorf("validate entity %s: %w", entity.Name, err)
	}

	srcRef := r.source.reference(entity, entity.sourceIdentifier())
	dstRef := r.destination.reference(entity, entity.destinationIdentifier())

	src, err := name.ParseReference(srcRef, nameOpts...)
	if err != nil {
		log.Warn().Err(err).Str("entity", entity.Name).Str("ref", srcRef).Msg("Skipping entity: failed to parse source ref")
		return fmt.Errorf("parse source ref %s: %w", srcRef, err)
	}

	dst, err := name.ParseReference(dstRef, nameOpts...)
	if err != nil {
		log.Warn().Err(err).Str("entity", entity.Name).Str("ref", dstRef).Msg("Skipping entity: failed to parse dest ref")
		return fmt.Errorf("parse dest ref %s: %w", dstRef, err)
	}

	desc, err := remote.Get(src, pullOpts...)
	if err != nil {
		log.Warn().Err(err).Str("entity", entity.Name).Str("ref", srcRef).Msg("Skipping entity: failed to fetch image descriptor")
		return fmt.Errorf("fetch image descriptor %s: %w", entity.Name, err)
	}

	img, err := desc.Image()
	if err != nil {
		log.Warn().Err(err).Str("entity", entity.Name).Msg("Skipping entity: failed to resolve image")
		return fmt.Errorf("resolve image %s: %w", entity.Name, err)
	}

	ociImage := mutate.MediaType(img, types.OCIManifestSchema1)

	srcDigest, err := ociImage.Digest()
	if err != nil {
		log.Warn().Err(err).Str("entity", entity.Name).Msg("Skipping entity: failed to compute source digest")
		return fmt.Errorf("compute source digest %s: %w", entity.Name, err)
	}

	dstDesc, dstErr := remote.Head(dst, pushOpts...)
	if dstErr == nil && dstDesc.Digest == srcDigest {
		log.Info().Msgf("Image %s already up-to-date at destination, skipping", entity.Name)
		return nil
	}

	srcLayers, err := ociImage.Layers()
	if err != nil {
		log.Warn().Err(err).Str("entity", entity.Name).Msg("Skipping entity: failed to get source layers")
		return fmt.Errorf("get source layers %s: %w", entity.Name, err)
	}

	missing := r.countMissingLayers(dst, srcLayers, pushOpts)
	log.Info().Msgf("Replicating image %s: %d/%d layers to pull", entity.Name, missing, len(srcLayers))

	if err := remote.Write(dst, ociImage, pushOpts...); err != nil {
		log.Warn().Err(err).Str("entity", entity.Name).Msg("Skipping entity: failed to replicate image")
		return fmt.Errorf("replicate image %s: %w", entity.Name, err)
	}
	log.Info().Msgf("Image %s replicated successfully", entity.Name)
	return nil
}

// countMissingLayers checks which source layers are absent from the destination
// by comparing against the existing image's layer digests (if any).
func (r *RegistryStore) countMissingLayers(dst name.Reference, srcLayers []v1.Layer, pushOpts []remote.Option) int {
	dstImg, err := remote.Image(dst, pushOpts...)
	if err != nil {
		// No image at destination, all layers are missing
		return len(srcLayers)
	}

	dstLayers, err := dstImg.Layers()
	if err != nil {
		return len(srcLayers)
	}

	existing := make(map[v1.Hash]struct{}, len(dstLayers))
	for _, l := range dstLayers {
		d, err := l.Digest()
		if err != nil {
			continue
		}
		existing[d] = struct{}{}
	}

	missing := 0
	for _, l := range srcLayers {
		d, err := l.Digest()
		if err != nil {
			missing++
			continue
		}
		if _, ok := existing[d]; !ok {
			missing++
		}
	}

	return missing
}

// Delete removes artifact manifests from the destination registry. Registry
// support for delete-by-reference is required by the configured endpoint.
func (r *RegistryStore) Delete(ctx context.Context, replicationEntity []Artifact) error {
	log := logger.FromContext(ctx)
	auth := authn.FromConfig(authn.AuthConfig{
		Username: r.destination.Username,
		Password: r.destination.Password,
	})

	options := []crane.Option{crane.WithAuth(auth), crane.WithContext(ctx)}
	if r.source.PlainHTTP {
		options = append(options, crane.Insecure)
	}

	for _, entity := range replicationEntity {
		// Check context cancellation before processing each image
		select {
		case <-ctx.Done():
			log.Warn().Err(ctx.Err()).Msg("Context cancelled, stopping deletion")
			return ctx.Err()
		default:
		}

		if err := entity.validate(); err != nil {
			return err
		}

		log.Info().Msgf("Deleting image %s from repository %s at registry %s with tag %s", entity.Name, r.destination.repositoryPath(entity), r.destination.Endpoint, entity.Tag)

		err := crane.Delete(r.destination.reference(entity, entity.destinationIdentifier()), options...)
		if err != nil {
			log.Error().Msgf("Failed to delete image: %v", err)
			return err
		}
		log.Info().Msgf("Image %s deleted successfully", entity.Name)
	}

	return nil
}

// buildTLSTransport builds the source registry transport when custom TLS
// material is configured. A nil transport selects the library default.
func (r *RegistryStore) buildTLSTransport() (http.RoundTripper, error) {
	if r.source.TLS.CertFile == "" && r.source.TLS.CAFile == "" {
		return nil, nil
	}

	cfg := &satTLS.Config{
		CertFile:   r.source.TLS.CertFile,
		KeyFile:    r.source.TLS.KeyFile,
		CAFile:     r.source.TLS.CAFile,
		SkipVerify: r.source.TLS.SkipVerify,
		MinVersion: tls.VersionTLS12,
	}

	tlsConfig, err := satTLS.LoadClientTLSConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("load TLS config: %w", err)
	}

	return &http.Transport{
		TLSClientConfig: tlsConfig,
	}, nil
}
