package store

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/logger"
	satTLS "github.com/container-registry/harbor-satellite/internal/satellite/tls"
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
func (r *RegistryStore) Replicate(ctx context.Context, replicationEntities []Artifact) error {
	log := logger.FromContext(ctx)
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

	for _, entity := range replicationEntities {
		// Check context cancellation before processing each image
		select {
		case <-ctx.Done():
			log.Warn().Err(ctx.Err()).Msg("Context cancelled, stopping replication")
			return ctx.Err()
		default:
		}

		if err := entity.validate(); err != nil {
			return err
		}

		srcRef := r.source.reference(entity, entity.sourceIdentifier())
		dstRef := r.destination.reference(entity, entity.destinationIdentifier())

		src, err := name.ParseReference(srcRef, nameOpts...)
		if err != nil {
			return fmt.Errorf("parse source ref %s: %w", srcRef, err)
		}

		dst, err := name.ParseReference(dstRef, nameOpts...)
		if err != nil {
			return fmt.Errorf("parse dest ref %s: %w", dstRef, err)
		}

		// Lazy fetch: only the manifest is downloaded, no layer data yet
		desc, err := remote.Get(src, pullOpts...)
		if err != nil {
			log.Error().Msgf("Failed to fetch image descriptor: %v", err)
			return err
		}

		img, err := desc.Image()
		if err != nil {
			log.Error().Msgf("Failed to resolve image: %v", err)
			return err
		}

		// Lazy OCI conversion, no data materialized
		ociImage := mutate.MediaType(img, types.OCIManifestSchema1)

		// Check if image already exists at destination with same digest
		srcDigest, err := ociImage.Digest()
		if err != nil {
			return fmt.Errorf("compute source digest: %w", err)
		}

		dstDesc, dstErr := remote.Head(dst, pushOpts...)
		if dstErr == nil && dstDesc.Digest == srcDigest {
			log.Info().Msgf("Image %s already up-to-date at destination, skipping", entity.Name)
			continue
		}

		// Log which layers need pulling vs already present
		srcLayers, err := ociImage.Layers()
		if err != nil {
			return fmt.Errorf("get source layers: %w", err)
		}

		missing := r.countMissingLayers(dst, srcLayers, pushOpts)
		log.Info().Msgf("Replicating image %s: %d/%d layers to pull", entity.Name, missing, len(srcLayers))

		// remote.Write streams layers one-by-one. For each layer it HEAD-checks
		// the destination first; only missing blobs are pulled from source.
		// Manifest is pushed last.
		if err := remote.Write(dst, ociImage, pushOpts...); err != nil {
			log.Error().Msgf("Failed to replicate image: %v", err)
			return err
		}
		log.Info().Msgf("Image %s replicated successfully", entity.Name)
	}

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
