package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/errdef"
)

// OCIStore persists content in one Satellite-owned OCI image layout.
type OCIStore struct {
	root       string
	store      *oci.Store
	mu         storeLock
	provenance sync.Map
}

// NewOCIStore opens or creates an OCI image-layout store at root.
func NewOCIStore(root string) (*OCIStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("OCI store root is required")
	}
	// A killed process cannot run the stage cleanup. This store is opened once,
	// before replication, so anything still under .stage belongs to that process.
	if err := clearStaleStages(root); err != nil {
		return nil, err
	}
	target, err := oci.New(root)
	if err != nil {
		return nil, fmt.Errorf("open OCI store at %s: %w", root, err)
	}
	return &OCIStore{root: root, store: target}, nil
}

// Pull resolves already retained content. It deliberately performs no request
// validation or remote access: proxy routing owns validation, and Replicate
// fills local content from the configured Harbor store.
func (s *OCIStore) Pull(ctx context.Context, artifact Artifact, resource PullResource) (ocispec.Descriptor, error) {
	s.mu.rLock()
	defer s.mu.rUnlock()

	switch resource {
	case PullResourceManifest:
		if desc, err := s.store.Resolve(ctx, artifact.Reference()); err == nil {
			return desc, nil
		} else if !errors.Is(err, errdef.ErrNotFound) {
			return ocispec.Descriptor{}, err
		}
		identifier := artifact.sourceIdentifier()
		if err := digest.Digest(identifier).Validate(); err != nil {
			return ocispec.Descriptor{}, errdef.ErrNotFound
		}
		desc, err := s.store.Resolve(ctx, identifier)
		if err != nil {
			return ocispec.Descriptor{}, err
		}
		known, err := s.repositoryGraphContains(ctx, artifact.Name, desc.Digest)
		if err != nil {
			return ocispec.Descriptor{}, err
		}
		if !known {
			return ocispec.Descriptor{}, errdef.ErrNotFound
		}
		return desc, nil
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
func (s *OCIStore) Fetch(ctx context.Context, _ Artifact, descriptor ocispec.Descriptor, _ PullResource, _ http.Header) (*http.Response, error) {
	s.mu.rLock()
	reader, err := s.store.Fetch(ctx, descriptor)
	if err != nil {
		s.mu.rUnlock()
		return nil, err
	}
	if seeker, ok := reader.(io.ReadSeekCloser); ok {
		return contentResponse(descriptor, &lockedReadSeekCloser{ReadSeekCloser: seeker, unlock: s.mu.rUnlock}), nil
	}
	return contentResponse(descriptor, &lockedReadCloser{ReadCloser: reader, unlock: s.mu.rUnlock}), nil
}

// Replicate copies complete manifest graphs or standalone blobs from source.
// The remote transfer is staged outside the serving lock, then published under
// that lock. Publish returns when ctx is canceled instead of waiting for an
// open local pull to finish.
func (s *OCIStore) Replicate(ctx context.Context, source Store, artifacts []Artifact) error {
	sourceTarget, ok := source.(storeTarget)
	if !ok {
		return errors.New("source store does not support OCI graph transfer")
	}
	for _, artifact := range artifacts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.replicateOne(ctx, source, sourceTarget, artifact); err != nil {
			return err
		}
	}
	return nil
}

func (s *OCIStore) replicateOne(ctx context.Context, source Store, sourceTarget storeTarget, artifact Artifact) (err error) {
	if err = artifact.validate(); err != nil {
		return err
	}
	remote, err := sourceTarget.targetFor(artifact)
	if err != nil {
		return err
	}
	staged, err := s.stage(ctx, source, remote, artifact)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, staged.cleanup())
	}()
	if err = s.mu.lock(ctx); err != nil {
		return err
	}
	defer s.mu.unlock()
	if err = ctx.Err(); err != nil {
		return err
	}
	return s.publish(ctx, staged, artifact)
}

type stagedArtifact struct {
	dir   string
	store *oci.Store
	desc  ocispec.Descriptor
}

func (s stagedArtifact) cleanup() error {
	if s.dir == "" {
		return nil
	}
	return os.RemoveAll(s.dir)
}

func (s *OCIStore) stage(ctx context.Context, source Store, remote oras.Target, artifact Artifact) (stagedArtifact, error) {
	dir, target, err := s.newStage()
	if err != nil {
		return stagedArtifact{}, err
	}
	desc, err := copyIntoStage(ctx, source, remote, target, s.store, dir, s.root, artifact)
	if err != nil {
		return stagedArtifact{}, errors.Join(err, os.RemoveAll(dir))
	}
	return stagedArtifact{dir: dir, store: target, desc: desc}, nil
}

// clearStaleStages removes copy directories left by a process that exited
// before its deferred cleanup. A live copy is removed when replicateOne returns.
func clearStaleStages(root string) error {
	parent := filepath.Join(root, ".stage")
	entries, err := os.ReadDir(parent)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read OCI staging directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "copy-") {
			continue
		}
		if err := os.RemoveAll(filepath.Join(parent, entry.Name())); err != nil {
			return fmt.Errorf("remove stale OCI stage %s: %w", entry.Name(), err)
		}
	}
	return nil
}

// newStage keeps the temporary layout on the store volume. A remote graph can
// be large, and the serving layout must not be the writer for that transfer.
func (s *OCIStore) newStage() (string, *oci.Store, error) {
	parent := filepath.Join(s.root, ".stage")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", nil, fmt.Errorf("create OCI staging directory: %w", err)
	}
	dir, err := os.MkdirTemp(parent, "copy-")
	if err != nil {
		return "", nil, fmt.Errorf("create OCI staging directory: %w", err)
	}
	target, err := oci.New(dir)
	if err != nil {
		return "", nil, errors.Join(fmt.Errorf("open OCI staging store: %w", err), os.RemoveAll(dir))
	}
	return dir, target, nil
}

func copyIntoStage(ctx context.Context, source Store, remote oras.Target, stage, present *oci.Store, stageRoot, presentRoot string, artifact Artifact) (ocispec.Descriptor, error) {
	// Blobs already in the serving store are hardlinked into the stage. oras
	// then skips the network fetch. The extra link keeps the bytes alive if
	// another group untags the last manifest and garbage-collects that path
	// before publish.
	dst := &stageTarget{Store: stage, present: present, stageRoot: stageRoot, presentRoot: presentRoot}
	if artifact.Tag == "" {
		desc, err := source.Pull(ctx, artifact, PullResourceBlob)
		if err != nil {
			return ocispec.Descriptor{}, err
		}
		if err := oras.CopyGraph(ctx, remote, dst, desc, oras.DefaultCopyGraphOptions); err != nil {
			return ocispec.Descriptor{}, fmt.Errorf("stage blob %s: %w", desc.Digest, err)
		}
		return desc, nil
	}
	desc, err := oras.Copy(ctx, remote, artifact.sourceIdentifier(), dst, artifact.Reference(), oras.DefaultCopyOptions)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("stage artifact %s: %w", artifact.Reference(), err)
	}
	return desc, nil
}

// stageTarget is the staging layout plus blobs hardlinked from the serving store.
// Exists is the only serving-store read. Push, fetch, and tag stay on the stage.
type stageTarget struct {
	*oci.Store
	present     *oci.Store
	presentRoot string
	stageRoot   string
	mu          sync.Mutex
	// pinned records descriptors whose whole successor graph is linked into the stage.
	pinned map[digest.Digest]struct{}
}

func (t *stageTarget) Exists(ctx context.Context, target ocispec.Descriptor) (bool, error) {
	inStage, err := t.Store.Exists(ctx, target)
	if err != nil {
		return false, err
	}
	if t.present == nil {
		return inStage, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.pinned[target.Digest]; ok {
		return true, nil
	}
	inPresent, err := t.present.Exists(ctx, target)
	if err != nil {
		return false, err
	}
	if !inPresent {
		return inStage, nil
	}
	if err := t.pin(ctx, target, map[digest.Digest]struct{}{}); err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, errdef.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// pin hardlinks target and every successor from the serving layout into the stage.
// oras skips a node and its successors once Exists is true, so a manifest link
// without its layers would let publish miss a blob that GC has removed.
func (t *stageTarget) pin(ctx context.Context, target ocispec.Descriptor, seen map[digest.Digest]struct{}) error {
	if _, ok := seen[target.Digest]; ok {
		return nil
	}
	seen[target.Digest] = struct{}{}
	if _, ok := t.pinned[target.Digest]; ok {
		return nil
	}
	if _, _, err := linkBlobFile(t.presentRoot, t.stageRoot, target.Digest); err != nil {
		return err
	}
	successors, err := content.Successors(ctx, t.present, target)
	if err != nil {
		return err
	}
	for _, node := range successors {
		if err := t.pin(ctx, node, seen); err != nil {
			return err
		}
	}
	if t.pinned == nil {
		t.pinned = make(map[digest.Digest]struct{})
	}
	t.pinned[target.Digest] = struct{}{}
	return nil
}

// linkBlobFile adds a directory entry for an existing blob. The stage and the
// serving layout share one filesystem, so the link does not copy the bytes.
// created is false when the destination name was already present.
func linkBlobFile(fromRoot, toRoot string, dgst digest.Digest) (dst string, created bool, err error) {
	if err = dgst.Validate(); err != nil {
		return "", false, fmt.Errorf("link blob %s: %w", dgst, err)
	}
	rel := filepath.Join(ocispec.ImageBlobsDir, dgst.Algorithm().String(), dgst.Encoded())
	src := filepath.Join(fromRoot, rel)
	dst = filepath.Join(toRoot, rel)
	if err = os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", false, fmt.Errorf("link blob %s: %w", dgst, err)
	}
	if err = os.Link(src, dst); err != nil {
		if errors.Is(err, os.ErrExist) {
			return dst, false, nil
		}
		return "", false, fmt.Errorf("link blob %s: %w", dgst, err)
	}
	return dst, true, nil
}

// PushReference makes the stage a reference pusher. oras.Copy uses that path
// when the root is already present, and tags only after the manifest bytes
// are in the stage. Layers that already exist are pinned in the stage.
func (t *stageTarget) PushReference(ctx context.Context, expected ocispec.Descriptor, reader io.Reader, reference string) error {
	if err := t.Push(ctx, expected, reader); err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
		return err
	}
	return t.Tag(ctx, expected, reference)
}

func (s *OCIStore) publish(ctx context.Context, staged stagedArtifact, artifact Artifact) error {
	// Linking keeps the write lock to a metadata update. oras.Copy would read
	// and write every new blob again while Pull and Fetch wait, and the stage
	// would still be on disk until this returns.
	if err := s.promote(ctx, staged.dir, staged.store, staged.desc); err != nil {
		return fmt.Errorf("publish %s to OCI store: %w", artifact.Reference(), err)
	}
	if err := s.store.Tag(ctx, staged.desc, artifact.Reference()); err != nil {
		return fmt.Errorf("tag %s in OCI store: %w", artifact.Reference(), err)
	}
	if artifact.Tag == "" {
		s.provenance.Store(provenanceKey(artifact.Name, staged.desc.Digest), struct{}{})
		return nil
	}
	if err := s.recordGraphProvenance(ctx, artifact.Name, staged.desc); err != nil {
		return err
	}
	logger.FromContext(ctx).Info().Str("reference", artifact.Reference()).Str("digest", staged.desc.Digest.String()).Msg("artifact replicated to OCI store")
	return nil
}

// promote links staged blobs into the serving layout, then Tag records the root
// in index.json. Blobs already in the serving store stay there. A failed
// promote removes only the links it added.
func (s *OCIStore) promote(ctx context.Context, stageRoot string, stage *oci.Store, root ocispec.Descriptor) (err error) {
	var created []string
	defer func() {
		if err == nil {
			return
		}
		for _, path := range created {
			removeErr := os.Remove(path)
			if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, fmt.Errorf("remove partial blob %s: %w", path, removeErr))
			}
		}
	}()
	return walkDescriptorGraph(ctx, stage, root, func(desc ocispec.Descriptor) error {
		exists, existsErr := s.store.Exists(ctx, desc)
		if existsErr != nil {
			return existsErr
		}
		if exists {
			return nil
		}
		blobPath := filepath.Join(stageRoot, ocispec.ImageBlobsDir, desc.Digest.Algorithm().String(), desc.Digest.Encoded())
		info, statErr := os.Stat(blobPath)
		if statErr != nil {
			return statErr
		}
		if desc.Size >= 0 && info.Size() != desc.Size {
			return fmt.Errorf("promote blob %s: stage has %d bytes, descriptor size is %d", desc.Digest, info.Size(), desc.Size)
		}
		dst, linked, linkErr := linkBlobFile(stageRoot, s.root, desc.Digest)
		if linkErr != nil {
			return linkErr
		}
		if linked {
			created = append(created, dst)
		}
		return nil
	})
}

func (s *OCIStore) targetFor(Artifact) (oras.Target, error) { //nolint:unparam // shared adapter permits registry construction errors.
	return s.store, nil
}

// Delete removes selected roots and garbage-collects unreachable blobs.
func (s *OCIStore) Delete(ctx context.Context, artifacts []Artifact) error {
	if err := s.mu.lock(ctx); err != nil {
		return err
	}
	defer s.mu.unlock()
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
