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

// Replicate replicates images from the source registry to the Zot registry.
// Uses lazy image references so layers are streamed one-by-one from source
// to destination. If the process crashes mid-replication, already-pushed
// layers are preserved and skipped on retry.
func (r *BasicReplicator) Replicate(ctx context.Context, replicationEntities []Entity) error {
	log := logger.FromContext(ctx)
	pullAuth := authn.FromConfig(authn.AuthConfig{
		Username: r.sourceUsername,
		Password: r.sourcePassword,
	})
	pushAuth := authn.FromConfig(authn.AuthConfig{
		Username: r.remoteUsername,
		Password: r.remotePassword,
	})

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
		}
	}

	for _, entity := range replicationEntities {
		srcRef := fmt.Sprintf("%s/%s/%s:%s", r.sourceRegistry, entity.GetRepository(), entity.GetName(), entity.GetTag())
		dstRef := fmt.Sprintf("%s/%s/%s:%s", r.remoteRegistryURL, entity.GetRepository(), entity.GetName(), entity.GetTag())

		log.Info().Msgf("Replicating image %s from %s to %s", entity.GetName(), r.sourceRegistry, r.remoteRegistryURL)

		src, err := name.ParseReference(srcRef, nameOpts...)
		if err != nil {
			return fmt.Errorf("parse source ref %s: %w", srcRef, err)
		}

		dst, err := name.ParseReference(dstRef, nameOpts...)
		if err != nil {
			return fmt.Errorf("parse dest ref %s: %w", dstRef, err)
		}

		// Lazy fetch - no layers downloaded yet
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

		// Lazy OCI conversion - no data materialized
		ociImage := mutate.MediaType(img, types.OCIManifestSchema1)

		// Streams layers one-by-one: pull layer from source, push to dest,
		// then move to next layer. Manifest is pushed last.
		if err := remote.Write(dst, ociImage, pushOpts...); err != nil {
			log.Error().Msgf("Failed to replicate image: %v", err)
			return err
		}
		log.Info().Msgf("Image %s replicated successfully", entity.GetName())
	}
	return nil
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
