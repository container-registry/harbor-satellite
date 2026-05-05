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
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
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
	syncCfg           config.SyncConfig
}

func NewBasicReplicator(sourceUsername, sourcePassword, sourceRegistry, remoteURL, remoteUsername, remotePassword string, useUnsecure bool) Replicator {
	return NewBasicReplicatorWithTLS(sourceUsername, sourcePassword, sourceRegistry, remoteURL, remoteUsername, remotePassword, useUnsecure, config.TLSConfig{}, config.SyncConfig{})
}

func NewBasicReplicatorWithTLS(sourceUsername, sourcePassword, sourceRegistry, remoteURL, remoteUsername, remotePassword string, useUnsecure bool, tlsCfg config.TLSConfig, syncCfg config.SyncConfig) Replicator {
	return &BasicReplicator{
		sourceUsername:    sourceUsername,
		sourcePassword:    sourcePassword,
		useUnsecure:       useUnsecure,
		remoteRegistryURL: remoteURL,
		sourceRegistry:    sourceRegistry,
		remoteUsername:    remoteUsername,
		remotePassword:    remotePassword,
		tlsCfg:            tlsCfg,
		syncCfg:           syncCfg,
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

// Replicate replicates images from the source registry to the local registry.
// Before pulling, it checks which blobs already exist at the destination and
// only downloads missing layers from source, saving bandwidth on crash recovery.
func (r *BasicReplicator) Replicate(ctx context.Context, replicationEntities []Entity) error {
	log := logger.FromContext(ctx)

	nameOpts, pullOpts, pushOpts, err := r.buildRemoteOptions(ctx)
	if err != nil {
		return err
	}

	for _, entity := range replicationEntities {
		select {
		case <-ctx.Done():
			log.Warn().Err(ctx.Err()).Msg("Context cancelled, stopping replication")
			return ctx.Err()
		default:
		}
		if err := r.replicateOne(ctx, entity, nameOpts, pullOpts, pushOpts); err != nil {
			return err
		}
	}
	return nil
}

func (r *BasicReplicator) buildRemoteOptions(ctx context.Context) ([]name.Option, []remote.Option, []remote.Option, error) {
	pullAuth := authn.FromConfig(authn.AuthConfig{Username: r.sourceUsername, Password: r.sourcePassword})
	pushAuth := authn.FromConfig(authn.AuthConfig{Username: r.remoteUsername, Password: r.remotePassword})

	var nameOpts []name.Option
	pullOpts := []remote.Option{remote.WithAuth(pullAuth), remote.WithContext(ctx)}
	pushOpts := []remote.Option{remote.WithAuth(pushAuth), remote.WithContext(ctx)}

	if r.useUnsecure {
		nameOpts = append(nameOpts, name.Insecure)
		if r.syncCfg.MaxBandwidthMbps > 0 {
			pullOpts = append(pullOpts, remote.WithTransport(newThrottledTransport(http.DefaultTransport, r.syncCfg.MaxBandwidthMbps)))
		}
		return nameOpts, pullOpts, pushOpts, nil
	}

	tlsTransport, err := r.buildTLSTransport()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build transport: %w", err)
	}
	if tlsTransport != nil {
		pushOpts = append(pushOpts, remote.WithTransport(tlsTransport))
	}
	var pullBase http.RoundTripper = http.DefaultTransport
	if tlsTransport != nil {
		pullBase = tlsTransport
	}
	if r.syncCfg.MaxBandwidthMbps > 0 {
		pullOpts = append(pullOpts, remote.WithTransport(newThrottledTransport(pullBase, r.syncCfg.MaxBandwidthMbps)))
	} else if tlsTransport != nil {
		pullOpts = append(pullOpts, remote.WithTransport(tlsTransport))
	}
	return nameOpts, pullOpts, pushOpts, nil
}

func (r *BasicReplicator) replicateOne(ctx context.Context, entity Entity, nameOpts []name.Option, pullOpts []remote.Option, pushOpts []remote.Option) error {
	log := logger.FromContext(ctx)

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

	srcDigest, err := ociImage.Digest()
	if err != nil {
		return fmt.Errorf("compute source digest: %w", err)
	}

	dstDesc, dstErr := remote.Head(dst, pushOpts...)
	if dstErr == nil && dstDesc.Digest == srcDigest {
		log.Info().Msgf("Image %s already up-to-date at destination, skipping", entity.GetName())
		return nil
	}

	srcLayers, err := ociImage.Layers()
	if err != nil {
		return fmt.Errorf("get source layers: %w", err)
	}

	missing := r.countMissingLayers(dst, srcLayers, pushOpts)
	log.Info().Msgf("Replicating image %s: %d/%d layers to pull", entity.GetName(), missing, len(srcLayers))

	// remote.Write streams layers one-by-one; HEAD-checks destination per blob,
	// only missing blobs are pulled. Manifest is pushed last.
	if err := remote.Write(dst, ociImage, pushOpts...); err != nil {
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

// buildTLSTransport returns an http.Transport with TLS configured, or nil when
// no TLS config is set (callers then use the default transport).
func (r *BasicReplicator) buildTLSTransport() (http.RoundTripper, error) {
	if r.tlsCfg.CertFile == "" && r.tlsCfg.CAFile == "" && !r.tlsCfg.SkipVerify {
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
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.TLSClientConfig = tlsConfig
	return base, nil
}
