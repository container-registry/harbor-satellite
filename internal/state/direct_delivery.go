package state

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// digestMapFile is the sidecar file that tracks which tarballs have been
// written and their source digests, to avoid redundant writes.
const digestMapFile = ".satellite-digests.json"

// DirectDeliverer writes Docker-save format tarballs into a directory that
// k3s/RKE2 watches for automatic import into the containerd image store.
type DirectDeliverer struct {
	imageDir    string
	useUnsecure bool
	srcUsername string
	srcPassword string
	srcRegistry string
}

// NewDirectDeliverer creates a deliverer that writes tarballs to imageDir.
func NewDirectDeliverer(imageDir, srcUsername, srcPassword, srcRegistry string, useUnsecure bool) *DirectDeliverer {
	return &DirectDeliverer{
		imageDir:    imageDir,
		useUnsecure: useUnsecure,
		srcUsername: srcUsername,
		srcPassword: srcPassword,
		srcRegistry: srcRegistry,
	}
}

// Deliver writes a Docker-save tarball for each entity into the image directory.
// Existing tarballs with matching digests are skipped.
func (d *DirectDeliverer) Deliver(ctx context.Context, entities []Entity) error {
	if len(entities) == 0 {
		return nil
	}

	log := logger.FromContext(ctx)
	digests := d.loadDigestMap()

	auth := authn.FromConfig(authn.AuthConfig{
		Username: d.srcUsername,
		Password: d.srcPassword,
	})

	var nameOpts []name.Option
	if d.useUnsecure {
		nameOpts = append(nameOpts, name.Insecure)
	}

	for _, entity := range entities {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		filename := tarballFilename(entity)

		// Skip if digest matches what we already wrote.
		if prev, ok := digests[filename]; ok && prev == entity.Digest {
			log.Debug().Str("file", filename).Msg("Direct delivery: tarball up-to-date, skipping")
			continue
		}

		srcRef := fmt.Sprintf("%s/%s/%s:%s", d.srcRegistry, entity.Repository, entity.Name, entity.Tag)
		ref, err := name.ParseReference(srcRef, nameOpts...)
		if err != nil {
			return fmt.Errorf("parse ref %s: %w", srcRef, err)
		}

		opts := []remote.Option{remote.WithAuth(auth), remote.WithContext(ctx)}
		img, err := remote.Image(ref, opts...)
		if err != nil {
			return fmt.Errorf("pull image %s: %w", srcRef, err)
		}

		dstPath := filepath.Join(d.imageDir, filename)
		if err := d.writeAtomically(dstPath, ref, img); err != nil {
			return fmt.Errorf("write tarball %s: %w", dstPath, err)
		}

		digests[filename] = entity.Digest
		log.Info().Str("file", filename).Str("ref", srcRef).Msg("Direct delivery: tarball written")
	}

	return d.saveDigestMap(digests)
}

// Delete removes tarballs for entities no longer in the desired state.
func (d *DirectDeliverer) Delete(ctx context.Context, entities []Entity) error {
	if len(entities) == 0 {
		return nil
	}

	log := logger.FromContext(ctx)
	digests := d.loadDigestMap()

	for _, entity := range entities {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		filename := tarballFilename(entity)
		path := filepath.Join(d.imageDir, filename)

		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Warn().Err(err).Str("file", filename).Msg("Direct delivery: failed to remove tarball")
			continue
		}

		delete(digests, filename)
		log.Info().Str("file", filename).Msg("Direct delivery: tarball removed")
	}

	return d.saveDigestMap(digests)
}

// writeAtomically writes the tarball to a temp file then renames it,
// preventing k3s from importing a partial file.
func (d *DirectDeliverer) writeAtomically(dstPath string, ref name.Reference, img v1.Image) error {
	tmp, err := os.CreateTemp(d.imageDir, ".satellite-*.tar.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if err := tarball.Write(ref, img, tmp); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write docker-save tarball: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpName, dstPath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename to final path: %w", err)
	}

	return nil
}

// tarballFilename produces a filesystem-safe filename for an entity.
// Format: {repository}_{name}_{tag}.tar with path separators replaced.
func tarballFilename(e Entity) string {
	safe := strings.NewReplacer("/", "_", ":", "_", " ", "_")
	return safe.Replace(fmt.Sprintf("%s_%s_%s.tar", e.Repository, e.Name, e.Tag))
}

func (d *DirectDeliverer) digestMapPath() string {
	return filepath.Join(d.imageDir, digestMapFile)
}

func (d *DirectDeliverer) loadDigestMap() map[string]string {
	data, err := os.ReadFile(d.digestMapPath())
	if err != nil {
		return make(map[string]string)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return make(map[string]string)
	}
	return m
}

func (d *DirectDeliverer) saveDigestMap(m map[string]string) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal digest map: %w", err)
	}
	return os.WriteFile(d.digestMapPath(), data, 0o644)
}
