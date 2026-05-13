package state

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/logger"
	satTLS "github.com/container-registry/harbor-satellite/internal/tls"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/rs/zerolog"
)

type Replicator interface {
	// Replicate copies images from the source registry to the local registry.
	Replicate(ctx context.Context, replicationEntities []Entity) error
	// DeleteReplicationEntity deletes the image from the local registry.
	DeleteReplicationEntity(ctx context.Context, replicationEntity []Entity) error
}

type BasicReplicator struct {
	useUnsecure       bool
	sourceUsername    string
	sourcePassword    string
	sourceRegistry    string
	remoteRegistryURL string
	remoteUsername    string
	remotePassword    string
	tlsCfg            config.TLSConfig
}

func NewBasicReplicator(sourceUsername, sourcePassword, sourceRegistry, remoteURL, remoteUsername, remotePassword string, useUnsecure bool) Replicator {
	return NewBasicReplicatorWithTLS(sourceUsername, sourcePassword, sourceRegistry, remoteURL, remoteUsername, remotePassword, useUnsecure, config.TLSConfig{})
}

func NewBasicReplicatorWithTLS(sourceUsername, sourcePassword, sourceRegistry, remoteURL, remoteUsername, remotePassword string, useUnsecure bool, tlsCfg config.TLSConfig) Replicator {
	return &BasicReplicator{
		sourceUsername:    sourceUsername,
		sourcePassword:    sourcePassword,
		useUnsecure:       useUnsecure,
		remoteRegistryURL: remoteURL,
		sourceRegistry:    sourceRegistry,
		remoteUsername:    remoteUsername,
		remotePassword:    remotePassword,
		tlsCfg:            tlsCfg,
	}
}

// Entity represents an image or artifact which needs to be handled by the replicator
type Entity struct {
	Name       string `json:"name"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Digest     string `json:"digest"`
}

func (e Entity) GetName() string {
	return e.Name
}

func (e Entity) GetRepository() string {
	return e.Repository
}

func (e Entity) GetTag() string {
	return e.Tag
}

// Replicate copies images from the source registry to the local registry using
// crane's puller/pusher pattern. Multi-arch indexes are rebuilt as OCI before
// push because the embedded Zot registry rejects Docker v2 manifests.
func (r *BasicReplicator) Replicate(ctx context.Context, replicationEntities []Entity) error {
	log := logger.FromContext(ctx)

	nameOpts, pullOpts, pushOpts, err := r.buildOpts(ctx)
	if err != nil {
		return err
	}

	puller, err := remote.NewPuller(pullOpts...)
	if err != nil {
		return fmt.Errorf("create puller: %w", err)
	}
	pusher, err := remote.NewPusher(pushOpts...)
	if err != nil {
		return fmt.Errorf("create pusher: %w", err)
	}

	for _, entity := range replicationEntities {
		select {
		case <-ctx.Done():
			log.Warn().Err(ctx.Err()).Msg("Context cancelled, stopping replication")
			return ctx.Err()
		default:
		}

		if err := r.replicateEntity(ctx, log, entity, nameOpts, pushOpts, puller, pusher); err != nil {
			return err
		}
	}
	return nil
}

// buildOpts assembles the auth, transport, and name options used by both pull
// and push sides. Pull and push auth can differ — source and destination
// registries are typically separate.
func (r *BasicReplicator) buildOpts(ctx context.Context) ([]name.Option, []remote.Option, []remote.Option, error) {
	pullAuth := authn.FromConfig(authn.AuthConfig{Username: r.sourceUsername, Password: r.sourcePassword})
	pushAuth := authn.FromConfig(authn.AuthConfig{Username: r.remoteUsername, Password: r.remotePassword})

	var nameOpts []name.Option
	pullOpts := []remote.Option{remote.WithAuth(pullAuth), remote.WithContext(ctx)}
	pushOpts := []remote.Option{remote.WithAuth(pushAuth), remote.WithContext(ctx)}

	if r.useUnsecure {
		nameOpts = append(nameOpts, name.Insecure)
		return nameOpts, pullOpts, pushOpts, nil
	}

	transport, err := r.buildTLSTransport()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build TLS transport: %w", err)
	}
	if transport != nil {
		pullOpts = append(pullOpts, remote.WithTransport(transport))
		pushOpts = append(pushOpts, remote.WithTransport(transport))
	}
	return nameOpts, pullOpts, pushOpts, nil
}

// replicateEntity parses refs, fetches the source descriptor, and dispatches
// to the index- or image-specific replication path.
func (r *BasicReplicator) replicateEntity(
	ctx context.Context, log *zerolog.Logger, entity Entity,
	nameOpts []name.Option, pushOpts []remote.Option,
	puller *remote.Puller, pusher *remote.Pusher,
) error {
	srcRef := fmt.Sprintf("%s/%s/%s:%s", r.sourceRegistry, entity.GetRepository(), entity.GetName(), entity.GetTag())
	dstRef := fmt.Sprintf("%s/%s/%s:%s", r.remoteRegistryURL, entity.GetRepository(), entity.GetName(), entity.GetTag())

	src, err := name.ParseReference(srcRef, nameOpts...)
	if err != nil {
		return fmt.Errorf("parse source ref %s: %w", srcRef, err)
	}
	dst, err := name.ParseReference(dstRef, nameOpts...)
	if err != nil {
		return fmt.Errorf("parse dest ref %s: %w", dstRef, err)
	}

	// Lazy fetch: only the manifest is downloaded, no layer data yet.
	desc, err := puller.Get(ctx, src)
	if err != nil {
		log.Error().Msgf("Failed to fetch image descriptor: %v", err)
		return err
	}

	if desc.MediaType.IsIndex() {
		return r.replicateIndex(ctx, log, entity, desc, dst, pusher, pushOpts)
	}
	return r.replicateImage(ctx, log, entity, desc, dst, pusher, pushOpts)
}

// replicateIndex rebuilds a multi-arch index with OCI media types on every
// referenced manifest and pushes it. Zot rejects Docker v2 manifests, so the
// source index (whose children are often Docker schema2) cannot be pushed
// as-is even though the index media type itself may already be OCI.
func (r *BasicReplicator) replicateIndex(
	ctx context.Context, log *zerolog.Logger, entity Entity,
	desc *remote.Descriptor, dst name.Reference,
	pusher *remote.Pusher, pushOpts []remote.Option,
) error {
	srcIdx, err := desc.ImageIndex()
	if err != nil {
		log.Error().Msgf("Failed to resolve image index: %v", err)
		return err
	}

	ociIdx, platforms, err := buildOCIIndex(srcIdx, entity.GetName())
	if err != nil {
		return err
	}

	dstDigest, err := ociIdx.Digest()
	if err != nil {
		return fmt.Errorf("compute destination index digest: %w", err)
	}
	if dstHead, dstErr := remote.Head(dst, pushOpts...); dstErr == nil && dstHead.Digest == dstDigest {
		log.Info().Msgf("Image %s already up-to-date at destination, skipping", entity.GetName())
		return nil
	}

	log.Info().Msgf("Replicating multi-arch image %s (%d platforms)", entity.GetName(), platforms)
	if err := pusher.Push(ctx, dst, ociIdx); err != nil {
		log.Error().Msgf("Failed to replicate image index: %v", err)
		return err
	}
	log.Info().Msgf("Image %s replicated successfully", entity.GetName())
	return nil
}

// buildOCIIndex walks the source index and emits a new index whose children
// have been rewritten to OCI media types.
func buildOCIIndex(srcIdx v1.ImageIndex, entityName string) (v1.ImageIndex, int, error) {
	manifest, err := srcIdx.IndexManifest()
	if err != nil {
		return nil, 0, fmt.Errorf("read source index manifest: %w", err)
	}

	ociIdx := mutate.IndexMediaType(empty.Index, types.OCIImageIndex)
	for _, m := range manifest.Manifests {
		addendum, err := toOCIAddendum(srcIdx, m, entityName)
		if err != nil {
			return nil, 0, err
		}
		ociIdx = mutate.AppendManifests(ociIdx, addendum)
	}
	return ociIdx, len(manifest.Manifests), nil
}

// toOCIAddendum loads one child manifest from the source index and returns it
// wrapped in an OCI-typed IndexAddendum. Nested indexes and unknown media
// types are rejected to keep the conversion bounded.
func toOCIAddendum(srcIdx v1.ImageIndex, m v1.Descriptor, entityName string) (mutate.IndexAddendum, error) {
	if !m.MediaType.IsImage() {
		if m.MediaType.IsIndex() {
			return mutate.IndexAddendum{}, fmt.Errorf("nested image indexes not supported (entity %s)", entityName)
		}
		return mutate.IndexAddendum{}, fmt.Errorf("unsupported manifest entry media type %s (entity %s)", m.MediaType, entityName)
	}

	img, err := srcIdx.Image(m.Digest)
	if err != nil {
		return mutate.IndexAddendum{}, fmt.Errorf("load child image %s: %w", m.Digest, err)
	}
	return mutate.IndexAddendum{
		Add: mutate.MediaType(img, types.OCIManifestSchema1),
		Descriptor: v1.Descriptor{
			MediaType: types.OCIManifestSchema1,
			Platform:  m.Platform,
		},
	}, nil
}

// replicateImage pushes a single-platform image, rewriting its manifest media
// type to OCI for Zot compatibility.
func (r *BasicReplicator) replicateImage(
	ctx context.Context, log *zerolog.Logger, entity Entity,
	desc *remote.Descriptor, dst name.Reference,
	pusher *remote.Pusher, pushOpts []remote.Option,
) error {
	img, err := desc.Image()
	if err != nil {
		log.Error().Msgf("Failed to resolve image: %v", err)
		return err
	}
	ociImage := mutate.MediaType(img, types.OCIManifestSchema1)

	srcDigest, err := ociImage.Digest()
	if err != nil {
		return fmt.Errorf("compute source digest: %w", err)
	}
	if dstHead, dstErr := remote.Head(dst, pushOpts...); dstErr == nil && dstHead.Digest == srcDigest {
		log.Info().Msgf("Image %s already up-to-date at destination, skipping", entity.GetName())
		return nil
	}

	srcLayers, err := ociImage.Layers()
	if err != nil {
		return fmt.Errorf("get source layers: %w", err)
	}
	missing := r.countMissingLayers(dst, srcLayers, pushOpts)
	log.Info().Msgf("Replicating image %s: %d/%d layers to pull", entity.GetName(), missing, len(srcLayers))

	if err := pusher.Push(ctx, dst, ociImage); err != nil {
		log.Error().Msgf("Failed to replicate image: %v", err)
		return err
	}
	log.Info().Msgf("Image %s replicated successfully", entity.GetName())
	return nil
}

// countMissingLayers checks which source layers are absent from the destination
// by comparing against the existing image's layer digests (if any).
func (r *BasicReplicator) countMissingLayers(dst name.Reference, srcLayers []v1.Layer, pushOpts []remote.Option) int {
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

func (r *BasicReplicator) DeleteReplicationEntity(ctx context.Context, replicationEntity []Entity) error {
	log := logger.FromContext(ctx)
	auth := authn.FromConfig(authn.AuthConfig{
		Username: r.remoteUsername,
		Password: r.remotePassword,
	})

	options := []crane.Option{crane.WithAuth(auth), crane.WithContext(ctx)}
	if r.useUnsecure {
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

		log.Info().Msgf("Deleting image %s from repository %s at registry %s with tag %s", entity.GetName(), entity.GetRepository(), r.remoteRegistryURL, entity.GetTag())

		err := crane.Delete(fmt.Sprintf("%s/%s/%s:%s", r.remoteRegistryURL, entity.GetRepository(), entity.GetName(), entity.GetTag()), options...)
		if err != nil {
			log.Error().Msgf("Failed to delete image: %v", err)
			return err
		}
		log.Info().Msgf("Image %s deleted successfully", entity.GetName())
	}

	return nil
}

func (r *BasicReplicator) buildTLSTransport() (http.RoundTripper, error) {
	if r.tlsCfg.CertFile == "" && r.tlsCfg.CAFile == "" {
		return nil, nil
	}

	cfg := &satTLS.Config{
		CertFile:   r.tlsCfg.CertFile,
		KeyFile:    r.tlsCfg.KeyFile,
		CAFile:     r.tlsCfg.CAFile,
		SkipVerify: r.tlsCfg.SkipVerify,
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
