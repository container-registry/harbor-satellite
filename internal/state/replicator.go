package state

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/policy"
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
	verifier          policy.Verifier
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

// VerificationConfig bundles optional TLS and signature verification settings.
type VerificationConfig struct {
	TLS      config.TLSConfig
	Verifier policy.Verifier
}

// NewBasicReplicatorWithVerifier creates a replicator that checks cosign
// signatures before pushing each image. Pass a zero VerificationConfig to
// disable both TLS and verification.
func NewBasicReplicatorWithVerifier(sourceUsername, sourcePassword, sourceRegistry, remoteURL, remoteUsername, remotePassword string, useUnsecure bool, vcfg VerificationConfig) Replicator {
	return &BasicReplicator{
		sourceUsername:    sourceUsername,
		sourcePassword:    sourcePassword,
		useUnsecure:       useUnsecure,
		remoteRegistryURL: remoteURL,
		sourceRegistry:    sourceRegistry,
		remoteUsername:    remoteUsername,
		remotePassword:    remotePassword,
		tlsCfg:            vcfg.TLS,
		verifier:          vcfg.Verifier,
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
// When a signature verifier is configured, each image is verified before push.
func (r *BasicReplicator) Replicate(ctx context.Context, replicationEntities []Entity) error {
	log := logger.FromContext(ctx)

	pullAuth := authn.FromConfig(authn.AuthConfig{Username: r.sourceUsername, Password: r.sourcePassword})
	pushAuth := authn.FromConfig(authn.AuthConfig{Username: r.remoteUsername, Password: r.remotePassword})

	var nameOpts []name.Option
	pullOpts := []remote.Option{remote.WithAuth(pullAuth), remote.WithContext(ctx)}
	pushOpts := []remote.Option{remote.WithAuth(pushAuth), remote.WithContext(ctx)}

	if r.useUnsecure {
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

type ociImageInfo struct {
	image  v1.Image
	digest v1.Hash
	layers []v1.Layer
}

// fetchOCIImage pulls the image descriptor, resolves the image, converts it to
// OCI media type, and returns the image together with its digest and layers.
func fetchOCIImage(src name.Reference, opts []remote.Option) (ociImageInfo, error) {
	desc, err := remote.Get(src, opts...)
	if err != nil {
		return ociImageInfo{}, err
	}
	img, err := desc.Image()
	if err != nil {
		return ociImageInfo{}, err
	}
	ociImage := mutate.MediaType(img, types.OCIManifestSchema1)
	digest, err := ociImage.Digest()
	if err != nil {
		return ociImageInfo{}, fmt.Errorf("compute source digest: %w", err)
	}
	layers, err := ociImage.Layers()
	if err != nil {
		return ociImageInfo{}, fmt.Errorf("get source layers: %w", err)
	}
	return ociImageInfo{image: ociImage, digest: digest, layers: layers}, nil
}

// isUpToDate returns true when the image at dst already matches srcDigest.
func isUpToDate(dst name.Reference, srcDigest v1.Hash, opts []remote.Option) bool {
	dstDesc, err := remote.Head(dst, opts...)
	return err == nil && dstDesc.Digest == srcDigest
}

// replicateOne handles a single entity: optional signature check, skip-if-current,
// layer dedup logging, and final push.
func (r *BasicReplicator) replicateOne(
	ctx context.Context,
	entity Entity,
	nameOpts []name.Option,
	pullOpts, pushOpts []remote.Option,
) error {
	log := logger.FromContext(ctx)

	srcRef := fmt.Sprintf("%s/%s/%s:%s", r.sourceRegistry, entity.GetRepository(), entity.GetName(), entity.GetTag())
	dstRef := fmt.Sprintf("%s/%s/%s:%s", r.remoteRegistryURL, entity.GetRepository(), entity.GetName(), entity.GetTag())

	if err := r.checkSignature(ctx, srcRef, entity.GetName()); err != nil {
		return err
	}

	src, err := name.ParseReference(srcRef, nameOpts...)
	if err != nil {
		return fmt.Errorf("parse source ref %s: %w", srcRef, err)
	}

	dst, err := name.ParseReference(dstRef, nameOpts...)
	if err != nil {
		return fmt.Errorf("parse dest ref %s: %w", dstRef, err)
	}

	info, err := fetchOCIImage(src, pullOpts)
	if err != nil {
		log.Error().Msgf("Failed to fetch image: %v", err)
		return err
	}

	if isUpToDate(dst, info.digest, pushOpts) {
		log.Info().Msgf("Image %s already up-to-date at destination, skipping", entity.GetName())
		return nil
	}

	missing := r.countMissingLayers(dst, info.layers, pushOpts)
	log.Info().Msgf("Replicating image %s: %d/%d layers to pull", entity.GetName(), missing, len(info.layers))

	// remote.Write streams layers one-by-one, HEAD-checking each blob at the
	// destination first; only missing blobs are pulled from source.
	if err := remote.Write(dst, info.image, pushOpts...); err != nil {
		log.Error().Msgf("Failed to replicate image: %v", err)
		return err
	}
	log.Info().Msgf("Image %s replicated successfully", entity.GetName())
	return nil
}

// checkSignature verifies the cosign signature of imageRef when a verifier is
// configured. Warn-mode failures are logged and replication continues.
func (r *BasicReplicator) checkSignature(ctx context.Context, imageRef, entityName string) error {
	if r.verifier == nil {
		return nil
	}
	log := logger.FromContext(ctx)
	err := r.verifier.Verify(ctx, imageRef, r.useUnsecure, r.sourceUsername, r.sourcePassword)
	if err == nil {
		return nil
	}
	if policy.IsWarnError(err) {
		log.Warn().Msgf("signature warning for %s: %v", entityName, err)
		return nil
	}
	return fmt.Errorf("signature check: %w", err)
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
