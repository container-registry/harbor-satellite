package state

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

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
	mu          sync.Mutex
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
// Existing tarballs with matching digests are skipped. Errors for individual
// entities are logged and skipped so that one failure does not block the rest.
func (d *DirectDeliverer) Deliver(ctx context.Context, entities []Entity) error {
	if len(entities) == 0 {
		return nil
	}

	log := logger.FromContext(ctx)

	// Snapshot current digests for skip checks (non-critical read).
	d.mu.Lock()
	currentDigests := d.loadDigestMap()
	d.mu.Unlock()

	auth := authn.FromConfig(authn.AuthConfig{
		Username: d.srcUsername,
		Password: d.srcPassword,
	})

	var nameOpts []name.Option
	if d.useUnsecure {
		nameOpts = append(nameOpts, name.Insecure)
	}

	// Collect successful writes to merge atomically at the end.
	updates := make(map[string]string)

	for _, entity := range entities {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		filename := tarballFilename(entity)

		// Skip if digest matches what we already wrote.
		if prev, ok := currentDigests[filename]; ok && prev == entity.Digest {
			log.Debug().Str("file", filename).Msg("Direct delivery: tarball up-to-date, skipping")
			continue
		}

		srcRef := fmt.Sprintf("%s/%s/%s:%s", d.srcRegistry, entity.Repository, entity.Name, entity.Tag)
		ref, err := name.ParseReference(srcRef, nameOpts...)
		if err != nil {
			log.Warn().Err(err).Str("ref", srcRef).Msg("Direct delivery: failed to parse reference, skipping")
			continue
		}

		opts := []remote.Option{remote.WithAuth(auth), remote.WithContext(ctx)}
		img, err := remote.Image(ref, opts...)
		if err != nil {
			log.Warn().Err(err).Str("ref", srcRef).Msg("Direct delivery: failed to pull image, skipping")
			continue
		}

		dstPath := filepath.Join(d.imageDir, filename)
		if err := d.writeAtomically(dstPath, ref, img); err != nil {
			log.Warn().Err(err).Str("file", filename).Msg("Direct delivery: failed to write tarball, skipping")
			continue
		}

		updates[filename] = entity.Digest
		log.Info().Str("file", filename).Str("ref", srcRef).Msg("Direct delivery: tarball written")
	}

	if len(updates) == 0 {
		return nil
	}

	// Merge updates atomically: re-load from disk under lock so concurrent
	// Deliver/Delete calls don't overwrite each other's changes.
	d.mu.Lock()
	defer d.mu.Unlock()
	digests := d.loadDigestMap()
	for k, v := range updates {
		digests[k] = v
	}
	return d.saveDigestMap(digests)
}

// Delete removes tarballs for entities no longer in the desired state.
func (d *DirectDeliverer) Delete(ctx context.Context, entities []Entity) error {
	if len(entities) == 0 {
		return nil
	}

	log := logger.FromContext(ctx)

	// Collect filenames that were successfully removed.
	var removed []string

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

		removed = append(removed, filename)
		log.Info().Str("file", filename).Msg("Direct delivery: tarball removed")
	}

	if len(removed) == 0 {
		return nil
	}

	// Merge deletions atomically: re-load from disk under lock so concurrent
	// Deliver/Delete calls don't overwrite each other's changes.
	d.mu.Lock()
	defer d.mu.Unlock()
	digests := d.loadDigestMap()
	for _, filename := range removed {
		delete(digests, filename)
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
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write docker-save tarball: %w", err)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpName, dstPath); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename to final path: %w", err)
	}

	return nil
}

// tarballFilename produces a filesystem-safe filename for an entity.
// Format: {repository}--{name}--{tag}.tar with path separators replaced by _.
// The -- delimiter is unambiguous because _ is used within fields for /.
func tarballFilename(e Entity) string {
	safe := strings.NewReplacer("/", "_", ":", "_", " ", "_")
	return fmt.Sprintf("%s--%s--%s.tar", safe.Replace(e.Repository), safe.Replace(e.Name), safe.Replace(e.Tag))
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
