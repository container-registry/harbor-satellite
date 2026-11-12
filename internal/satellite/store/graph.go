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

// manifestDescriptor reports whether desc is a manifest or an index the proxy
// can serve from the manifest endpoint.
func manifestDescriptor(desc ocispec.Descriptor) bool {
	switch desc.MediaType {
	case ocispec.MediaTypeImageManifest,
		ocispec.MediaTypeImageIndex,
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.artifact.manifest.v1+json":
		return true
	default:
		return false
	}
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

// GraphDigests lists the root descriptor and every successor digest. Schema 1
// manifests have no successor list, so only the manifest digest is returned.
// A layer named by a local manifest is included even when its bytes are absent.
func GraphDigests(ctx context.Context, storage Store, artifact Artifact) ([]string, error) {
	fetcher, err := graphFetcher(storage, artifact)
	if err != nil {
		return nil, err
	}
	root, err := resolveGraphRoot(ctx, storage, artifact)
	if err != nil {
		return nil, err
	}
	var digests []string
	err = walkDescriptorGraph(ctx, fetcher, root, func(desc ocispec.Descriptor) error {
		if desc.Digest == "" {
			return nil
		}
		digests = append(digests, desc.Digest.String())
		return nil
	})
	if err != nil {
		return nil, err
	}
	return digests, nil
}

func resolveGraphRoot(ctx context.Context, storage Store, artifact Artifact) (ocispec.Descriptor, error) {
	unwrapped := unwrapGraphStore(storage)
	if local, ok := unwrapped.(*OCIStore); ok && local != nil {
		return local.graphRoot(ctx, artifact)
	}
	if unwrapped == nil {
		return ocispec.Descriptor{}, errdef.ErrNotFound
	}
	lookup := artifact
	if artifact.Digest != "" {
		lookup.Tag = ""
	}
	return unwrapped.Pull(ctx, lookup, PullResourceManifest)
}

// graphRoot resolves the manifest the group named. The tag is used when it
// already points at that digest. Otherwise the digest blob is used, including
// when the repository and name are stored as separate fields.
func (s *OCIStore) graphRoot(ctx context.Context, artifact Artifact) (ocispec.Descriptor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if artifact.Tag != "" {
		desc, err := s.store.Resolve(ctx, artifact.Reference())
		if err == nil && (artifact.Digest == "" || desc.Digest.String() == artifact.sourceIdentifier()) {
			return desc, nil
		}
		if err != nil && !errors.Is(err, errdef.ErrNotFound) {
			return ocispec.Descriptor{}, err
		}
	}
	if artifact.Digest == "" {
		return ocispec.Descriptor{}, errdef.ErrNotFound
	}
	return s.store.Resolve(ctx, artifact.sourceIdentifier())
}

func graphFetcher(storage Store, artifact Artifact) (content.Fetcher, error) {
	storage = unwrapGraphStore(storage)
	target, ok := storage.(storeTarget)
	if !ok || target == nil {
		return nil, errors.New("store does not expose an OCI graph")
	}
	resolved, err := target.targetFor(artifact)
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		return nil, errors.New("store does not expose an OCI graph")
	}
	return resolved, nil
}

func unwrapGraphStore(storage Store) Store {
	for range 4 {
		switch current := storage.(type) {
		case *PeerFillingStore:
			if current == nil || current.local == nil {
				return storage
			}
			storage = current.local
		case labeledStore:
			if current.Store == nil {
				return storage
			}
			storage = current.Store
		default:
			return storage
		}
	}
	return storage
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
