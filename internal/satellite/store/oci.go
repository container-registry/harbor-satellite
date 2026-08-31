package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/logger"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/errdef"
)

// OCIStore persists content in one Satellite-owned OCI image layout.
type OCIStore struct {
	store      *oci.Store
	mu         sync.RWMutex
	provenance sync.Map
}

// NewOCIStore opens or creates an OCI image-layout store at root.
func NewOCIStore(root string) (*OCIStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("OCI store root is required")
	}
	target, err := oci.New(root)
	if err != nil {
		return nil, fmt.Errorf("open OCI store at %s: %w", root, err)
	}
	return &OCIStore{store: target}, nil
}

// Pull resolves already retained content. It deliberately performs no request
// validation or remote access: proxy routing owns validation, and Replicate
// fills local content from the configured Harbor store.
func (s *OCIStore) Pull(ctx context.Context, artifact Artifact, resource PullResource) (ocispec.Descriptor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	switch resource {
	case PullResourceManifest:
		return s.store.Resolve(ctx, artifact.Reference())
	case PullResourceBlob:
		if desc, err := s.store.Resolve(ctx, artifact.Reference()); err == nil {
			return desc, nil
		} else if !errors.Is(err, errdef.ErrNotFound) {
			return ocispec.Descriptor{}, err
		}
		desc, err := s.store.Resolve(ctx, artifact.sourceIdentifier())
		if err != nil {
			return ocispec.Descriptor{}, err
		}
		known, err := s.repositoryGraphContains(ctx, artifact.Name, desc.Digest)
		if err != nil || !known {
			if err != nil {
				return ocispec.Descriptor{}, err
			}
			return ocispec.Descriptor{}, errdef.ErrNotFound
		}
		return desc, nil
	default:
		return ocispec.Descriptor{}, errdef.ErrNotFound
	}
}

// Fetch opens one independent local stream. Holding the read lock until Close
// prevents Delete and GC from removing a blob while it is being served.
func (s *OCIStore) Fetch(ctx context.Context, _ Artifact, descriptor ocispec.Descriptor) (io.ReadCloser, error) {
	s.mu.RLock()
	reader, err := s.store.Fetch(ctx, descriptor)
	if err != nil {
		s.mu.RUnlock()
		return nil, err
	}
	if seeker, ok := reader.(io.ReadSeekCloser); ok {
		return &lockedReadSeekCloser{ReadSeekCloser: seeker, unlock: s.mu.RUnlock}, nil
	}
	return &lockedReadCloser{ReadCloser: reader, unlock: s.mu.RUnlock}, nil
}

// Replicate copies complete manifest graphs or standalone blobs from source.
// ORAS streams through its ingest path and commits only verified descriptors.
func (s *OCIStore) Replicate(ctx context.Context, source Store, artifacts []Artifact) error {
	sourceTarget, ok := source.(storeTarget)
	if !ok {
		return errors.New("source store does not support OCI graph transfer")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, artifact := range artifacts {
		if err := artifact.validate(); err != nil {
			return err
		}
		remote, err := sourceTarget.targetFor(artifact)
		if err != nil {
			return err
		}
		if artifact.Tag == "" {
			desc, err := source.Pull(ctx, artifact, PullResourceBlob)
			if err != nil {
				return err
			}
			if err := oras.CopyGraph(ctx, remote, s.store, desc, oras.DefaultCopyGraphOptions); err != nil {
				return fmt.Errorf("copy blob %s to OCI store: %w", desc.Digest, err)
			}
			if err := s.store.Tag(ctx, desc, artifact.Reference()); err != nil {
				return fmt.Errorf("tag local blob %s: %w", desc.Digest, err)
			}
			s.provenance.Store(provenanceKey(artifact.Name, desc.Digest), struct{}{})
			continue
		}

		desc, err := oras.Copy(ctx, remote, artifact.sourceIdentifier(), s.store, artifact.Reference(), oras.DefaultCopyOptions)
		if err != nil {
			return fmt.Errorf("copy artifact %s to OCI store: %w", artifact.Reference(), err)
		}
		if err := s.recordGraphProvenance(ctx, artifact.Name, desc); err != nil {
			return err
		}
		logger.FromContext(ctx).Info().Str("reference", artifact.Reference()).Str("digest", desc.Digest.String()).Msg("artifact replicated to OCI store")
	}
	return nil
}

func (s *OCIStore) targetFor(Artifact) (oras.Target, error) { //nolint:unparam // shared adapter permits registry construction errors.
	return s.store, nil
}

// Delete removes selected roots and garbage-collects unreachable blobs.
func (s *OCIStore) Delete(ctx context.Context, artifacts []Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for _, artifact := range artifacts {
		if err := artifact.validate(); err != nil {
			return err
		}
		if _, err := s.store.Resolve(ctx, artifact.Reference()); err != nil {
			if errors.Is(err, errdef.ErrNotFound) {
				continue
			}
			return err
		}
		if err := s.store.Untag(ctx, artifact.Reference()); err != nil {
			return err
		}
		changed = true
	}
	if changed {
		if err := s.store.GC(ctx); err != nil {
			return err
		}
		s.provenance.Clear()
	}
	return nil
}

func (s *OCIStore) recordGraphProvenance(ctx context.Context, repository string, root ocispec.Descriptor) error {
	return walkDescriptorGraph(ctx, s.store, root, func(desc ocispec.Descriptor) error {
		s.provenance.Store(provenanceKey(repository, desc.Digest), struct{}{})
		return nil
	})
}

func (s *OCIStore) repositoryGraphContains(ctx context.Context, repository string, wanted digest.Digest) (bool, error) {
	if _, known := s.provenance.Load(provenanceKey(repository, wanted)); known {
		return true, nil
	}
	var refs []string
	err := s.store.Tags(ctx, "", func(tags []string) error {
		for _, tag := range tags {
			base := strings.Trim(repository, "/")
			if strings.HasPrefix(tag, base+":") || strings.HasPrefix(tag, base+"@") {
				refs = append(refs, tag)
			}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	for _, ref := range refs {
		root, err := s.store.Resolve(ctx, ref)
		if err != nil {
			return false, err
		}
		err = walkDescriptorGraph(ctx, s.store, root, func(desc ocispec.Descriptor) error {
			if desc.Digest == wanted {
				return errGraphWalkComplete
			}
			return nil
		})
		if errors.Is(err, errGraphWalkComplete) {
			s.provenance.Store(provenanceKey(repository, wanted), struct{}{})
			return true, nil
		}
		if err != nil {
			return false, err
		}
	}
	return false, nil
}

var errGraphWalkComplete = errors.New("descriptor graph walk complete")

func walkDescriptorGraph(ctx context.Context, fetcher content.Fetcher, root ocispec.Descriptor, visit func(ocispec.Descriptor) error) error {
	queue := []ocispec.Descriptor{root}
	seen := make(map[digest.Digest]struct{})
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		desc := queue[0]
		queue = queue[1:]
		if _, ok := seen[desc.Digest]; ok {
			continue
		}
		seen[desc.Digest] = struct{}{}
		if err := visit(desc); err != nil {
			return err
		}
		successors, err := content.Successors(ctx, fetcher, desc)
		if err != nil {
			return err
		}
		queue = append(queue, successors...)
	}
	return nil
}

func provenanceKey(repository string, contentDigest digest.Digest) string {
	return strings.Trim(repository, "/") + "\x00" + contentDigest.String()
}

type lockedReadCloser struct {
	io.ReadCloser
	once   sync.Once
	unlock func()
}

func (r *lockedReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.once.Do(r.unlock)
	return err
}

type lockedReadSeekCloser struct {
	io.ReadSeekCloser
	once   sync.Once
	unlock func()
}

func (r *lockedReadSeekCloser) Close() error {
	err := r.ReadSeekCloser.Close()
	r.once.Do(r.unlock)
	return err
}
