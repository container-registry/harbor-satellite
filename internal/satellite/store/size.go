package store

import (
	"context"
	"errors"
	"fmt"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
)

// ErrUnsized means the stores cannot walk the image graph, so the caller
// cannot know how many bytes the copy will add.
var ErrUnsized = errors.New("image size is unavailable")

// MissingBytes sums the descriptors the destination does not already store.
// Blobs ORAS will skip are not included.
func MissingBytes(ctx context.Context, destination, source Store, artifact Artifact) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	from, err := graphTarget(source, artifact)
	if err != nil {
		return 0, err
	}
	to, err := graphTarget(destination, artifact)
	if err != nil {
		return 0, err
	}

	root, err := resolveCopyRoot(ctx, source, artifact)
	if err != nil {
		return 0, fmt.Errorf("resolve %s: %w", artifact.Reference(), err)
	}
	if err := rejectUnlistedDigest(root, artifact); err != nil {
		return 0, err
	}

	var missing int64
	err = walkDescriptorGraph(ctx, from, root, func(desc ocispec.Descriptor) error {
		exists, existsErr := to.Exists(ctx, desc)
		if existsErr != nil {
			return existsErr
		}
		if exists || desc.Size <= 0 {
			return nil
		}
		missing += desc.Size
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("size %s: %w", artifact.Reference(), err)
	}
	return missing, nil
}

func resolveCopyRoot(ctx context.Context, source Store, artifact Artifact) (ocispec.Descriptor, error) {
	if artifact.Tag == "" {
		return source.Pull(ctx, artifact, PullResourceBlob)
	}
	lookup := artifact
	if artifact.Digest != "" {
		lookup.Tag = ""
	}
	return source.Pull(ctx, lookup, PullResourceManifest)
}

func graphTarget(storage Store, artifact Artifact) (oras.Target, error) {
	if filling, ok := storage.(*PeerFillingStore); ok {
		storage = filling.local
	}
	target, ok := storage.(storeTarget)
	if !ok || target == nil {
		return nil, ErrUnsized
	}
	resolved, err := target.targetFor(artifact)
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		return nil, ErrUnsized
	}
	return resolved, nil
}
