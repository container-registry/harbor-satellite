package store

import (
	"context"
	"errors"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
)

// graphStorage is the read surface a completeness walk needs.
type graphStorage interface {
	content.Fetcher
	Exists(ctx context.Context, target ocispec.Descriptor) (bool, error)
}

// descriptorGraphReady reports ErrNotFound when root or any successor is absent.
func descriptorGraphReady(ctx context.Context, storage graphStorage, root ocispec.Descriptor) error {
	err := walkDescriptorGraph(ctx, storage, root, func(desc ocispec.Descriptor) error {
		exists, existsErr := storage.Exists(ctx, desc)
		if existsErr != nil {
			return existsErr
		}
		if !exists {
			return errdef.ErrNotFound
		}
		return nil
	})
	if errors.Is(err, errdef.ErrNotFound) {
		return errdef.ErrNotFound
	}
	return err
}

// ReadyManifest resolves a manifest from local. Stores that can walk a
// descriptor graph do so. Other stores resolve the manifest and, when artifact
// names a digest, require that digest.
func ReadyManifest(ctx context.Context, local Store, artifact Artifact) (ocispec.Descriptor, error) {
	if local == nil {
		return ocispec.Descriptor{}, errdef.ErrNotFound
	}
	if checker, ok := readyChecker(local); ok {
		return checker.Ready(ctx, artifact)
	}
	lookup := artifact
	if lookup.Tag != "" {
		lookup.Digest = ""
	}
	descriptor, err := local.Pull(ctx, lookup, PullResourceManifest)
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	if artifact.Digest != "" && descriptor.Digest.String() != artifact.sourceIdentifier() {
		return ocispec.Descriptor{}, errdef.ErrNotFound
	}
	return descriptor, nil
}

func readyChecker(local Store) (ReadyChecker, bool) {
	if local == nil {
		return nil, false
	}
	if checker, ok := local.(ReadyChecker); ok {
		return checker, true
	}
	filling, ok := local.(*PeerFillingStore)
	if !ok {
		return nil, false
	}
	return readyChecker(filling.local)
}
