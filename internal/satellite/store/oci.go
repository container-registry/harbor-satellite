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
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/telemetry"
	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/errdef"
)

// OCIStore persists content in one Satellite-owned OCI image layout.
//
// Locking (ADR-0013): oras-go v2.6.2 content/oci takes only its own read lock
// for Fetch, Push, Tag and Resolve, and a pushed blob appears by rename after
// its digest is verified, so reads by digest are safe next to writes. mu is
// the index lock: shared for resolving and for opening a reader, exclusive
// for tag changes, publish and collection. An open body holds no lock; the
// reference counter keeps collection away from it instead.
type OCIStore struct {
	root              string
	store             *oci.Store
	opts              OCIStoreOptions
	readers           *RefCounter
	mu                sync.RWMutex
	provenance        sync.Map
	beforePublishLock func()

	stageMu      sync.Mutex
	activeStages int

	gcMu              sync.Mutex
	unreferencedSince map[digest.Digest]time.Time

	streamsMu sync.Mutex
	streams   map[digest.Digest]chan struct{}
}

// NewOCIStore opens or creates an OCI image-layout store at root.
func NewOCIStore(root string) (*OCIStore, error) {
	s, _, err := OpenOCIStore(root, OCIStoreOptions{})
	return s, err
}

// layout returns the oras store for work that runs outside the index lock.
func (s *OCIStore) layout() *oci.Store {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.store
}

// Ready resolves a retained manifest and requires every successor blob to be
// present. A digest on artifact must match the tagged root.
func (s *OCIStore) Ready(ctx context.Context, artifact Artifact) (ocispec.Descriptor, error) {
	lookup := artifact
	if lookup.Tag != "" {
		lookup.Digest = ""
	}
	descriptor, err := s.Pull(ctx, lookup, PullResourceManifest)
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	if artifact.Digest != "" && descriptor.Digest.String() != artifact.sourceIdentifier() {
		return ocispec.Descriptor{}, errdef.ErrNotFound
	}
	if err := s.graphReady(ctx, descriptor); err != nil {
		return ocispec.Descriptor{}, err
	}
	return descriptor, nil
}

func (s *OCIStore) graphReady(ctx context.Context, root ocispec.Descriptor) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return descriptorGraphReady(ctx, s.store, root)
}

// Pull resolves already retained content. It deliberately performs no request
// validation or remote access: proxy routing owns validation, and Replicate
// fills local content from the configured Harbor store.
func (s *OCIStore) Pull(ctx context.Context, artifact Artifact, resource PullResource) (ocispec.Descriptor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

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

// Fetch opens one independent local stream. The body holds a reader
// reference until Close, which keeps collection from deleting the blob; it
// holds no store lock, so tag changes and publishes do not wait for it.
func (s *OCIStore) Fetch(ctx context.Context, _ Artifact, descriptor ocispec.Descriptor, _ PullResource, _ http.Header) (*http.Response, error) {
	release := s.readers.Acquire(descriptor.Digest)
	s.mu.RLock()
	reader, err := s.store.Fetch(ctx, descriptor)
	s.mu.RUnlock()
	if err != nil {
		release()
		return nil, err
	}
	if seeker, ok := reader.(io.ReadSeekCloser); ok {
		return contentResponse(descriptor, &releasingReadSeekCloser{ReadSeekCloser: seeker, release: release}), nil
	}
	return contentResponse(descriptor, &releasingReadCloser{ReadCloser: reader, release: release}), nil
}

// Replicate copies complete manifest graphs or standalone blobs from source.
// The remote transfer is staged outside the index lock, then published under
// it; publishing links files and tags, and does not wait for open bodies.
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
	started := time.Now()
	var published int64
	defer func() {
		outcome := "ok"
		if err != nil {
			outcome = "error"
		}
		s.recorder().ObserveReplication(published, time.Since(started), outcome)
	}()
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
	if err = ctx.Err(); err != nil {
		return err
	}
	if s.beforePublishLock != nil {
		s.beforePublishLock()
	}
	unlock := s.lockIndex("publish")
	defer unlock()
	if err = ctx.Err(); err != nil {
		return err
	}
	if published, err = s.publish(ctx, staged, artifact); err != nil {
		return err
	}
	if onWin := peerRaceOnWin(ctx); onWin != nil {
		onWin()
	}
	return nil
}

type stagedArtifact struct {
	owner *OCIStore
	dir   string
	store *oci.Store
	desc  ocispec.Descriptor
}

func (s stagedArtifact) cleanup() error {
	if s.dir == "" {
		return nil
	}
	return s.owner.removeStage(s.dir)
}

func (s *OCIStore) stage(ctx context.Context, source Store, remote oras.Target, artifact Artifact) (stagedArtifact, error) {
	dir, target, err := s.newStage()
	if err != nil {
		return stagedArtifact{}, err
	}
	desc, err := copyIntoStage(ctx, source, remote, target, s.layout(), dir, s.root, artifact)
	if err != nil {
		return stagedArtifact{}, errors.Join(err, s.removeStage(dir))
	}
	return stagedArtifact{owner: s, dir: dir, store: target, desc: desc}, nil
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
// The .stage parent exists only while a stage does, so the layout root holds
// nothing but OCI files between copies.
func (s *OCIStore) newStage() (string, *oci.Store, error) {
	parent := filepath.Join(s.root, ".stage")
	s.stageMu.Lock()
	if err := os.MkdirAll(parent, 0o700); err != nil {
		s.stageMu.Unlock()
		return "", nil, fmt.Errorf("create OCI staging directory: %w", err)
	}
	dir, err := os.MkdirTemp(parent, "copy-")
	if err != nil {
		s.stageMu.Unlock()
		return "", nil, fmt.Errorf("create OCI staging directory: %w", err)
	}
	s.activeStages++
	s.stageMu.Unlock()
	target, err := oci.New(dir)
	if err != nil {
		return "", nil, errors.Join(fmt.Errorf("open OCI staging store: %w", err), s.removeStage(dir))
	}
	return dir, target, nil
}

func (s *OCIStore) removeStage(dir string) error {
	err := os.RemoveAll(dir)
	s.stageMu.Lock()
	defer s.stageMu.Unlock()
	s.activeStages--
	if s.activeStages == 0 {
		if rmErr := os.Remove(filepath.Dir(dir)); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove OCI staging directory: %w", rmErr))
		}
	}
	return err
}

func copyIntoStage(ctx context.Context, source Store, remote oras.Target, stage, present *oci.Store, stageRoot, presentRoot string, artifact Artifact) (ocispec.Descriptor, error) {
	// Blobs already in the serving store are linked into the stage. oras
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

// stageTarget is the staging layout plus blobs linked from the serving store.
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

// publish returns the bytes it linked into the layout, which are the bytes
// this replication added.
func (s *OCIStore) publish(ctx context.Context, staged stagedArtifact, artifact Artifact) (int64, error) {
	// The stage is the quarantine. A root the group list did not ask for is
	// discarded here, before any blob is linked into the store that clients pull.
	if err := rejectUnlistedDigest(staged.desc, artifact); err != nil {
		return 0, err
	}
	// Linking keeps the write lock to a metadata update. oras.Copy would read
	// and write every new blob again while Pull and Fetch wait, and the stage
	// would still be on disk until this returns.
	linked, err := s.promote(ctx, staged.dir, staged.store, staged.desc)
	if err != nil {
		return 0, fmt.Errorf("publish %s to OCI store: %w", artifact.Reference(), err)
	}
	if err := s.store.Tag(ctx, staged.desc, artifact.Reference()); err != nil {
		return 0, fmt.Errorf("tag %s in OCI store: %w", artifact.Reference(), err)
	}
	if err := s.persistIndexLocked(ctx); err != nil {
		return 0, fmt.Errorf("persist index after tagging %s: %w", artifact.Reference(), err)
	}
	if artifact.Tag == "" {
		s.provenance.Store(provenanceKey(artifact.Name, staged.desc.Digest), struct{}{})
		return linked, nil
	}
	if err := s.recordGraphProvenance(ctx, artifact.Name, staged.desc); err != nil {
		return 0, err
	}
	logger.FromContext(ctx).Info().Str("reference", artifact.Reference()).Str("digest", staged.desc.Digest.String()).Msg("artifact replicated to OCI store")
	return linked, nil
}

// promote links staged blobs into the serving layout, then Tag records the root
// in index.json. Blobs already in the serving store stay there. A failed
// promote removes only the links it added.
func (s *OCIStore) promote(ctx context.Context, stageRoot string, stage *oci.Store, root ocispec.Descriptor) (linkedBytes int64, err error) {
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
	err = walkDescriptorGraph(ctx, stage, root, func(desc ocispec.Descriptor) error {
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
			linkedBytes += info.Size()
		}
		return nil
	})
	return linkedBytes, err
}

func (s *OCIStore) targetFor(Artifact) (oras.Target, error) { //nolint:unparam // shared adapter permits registry construction errors.
	return s.layout(), nil
}

// lockIndex takes the exclusive index lock and reports how long it waited.
func (s *OCIStore) lockIndex(operation string) (unlock func()) {
	started := time.Now()
	s.mu.Lock()
	s.recorder().ObserveStoreLockWait(operation, time.Since(started))
	return s.mu.Unlock
}

func (s *OCIStore) recorder() telemetry.Recorder {
	if s.opts.Recorder == nil {
		return telemetry.Nop{}
	}
	return s.opts.Recorder
}

// Delete untags the selected roots and collects what nothing references any
// more, within the store's grace period.
func (s *OCIStore) Delete(ctx context.Context, artifacts []Artifact) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(artifacts) == 0 {
		return nil
	}
	if err := s.untag(ctx, artifacts); err != nil {
		return err
	}
	// Collect also when nothing was untagged: the tag may have moved or been
	// removed before, leaving the old digest's content behind.
	_, err := s.CollectGarbage(ctx, GCOptions{
		Grace:        s.opts.GCGrace,
		Roots:        []GCRoots{s.opts.FillLeases},
		IngestMaxAge: defaultIngestMaxAge,
	})
	return err
}

// DeletesSingleTags is true: Delete untags one reference and collects only
// content no remaining tag references.
func (s *OCIStore) DeletesSingleTags() bool { return true }

func (s *OCIStore) untag(ctx context.Context, artifacts []Artifact) error {
	unlock := s.lockIndex("untag")
	defer unlock()
	for _, artifact := range artifacts {
		if err := artifact.validate(); err != nil {
			return err
		}
		desc, err := s.store.Resolve(ctx, artifact.Reference())
		if errors.Is(err, errdef.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if artifact.Digest != "" && desc.Digest.String() != artifact.Digest {
			// A newer replication moved the tag; it must stay.
			continue
		}
		if err := s.store.Untag(ctx, artifact.Reference()); err != nil {
			return err
		}
	}
	if err := s.persistIndexLocked(ctx); err != nil {
		return fmt.Errorf("persist index after untagging: %w", err)
	}
	// Provenance caches "repository holds digest"; untagged content must not
	// stay pullable through it until the collection removes the files.
	s.provenance.Clear()
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

type releasingReadCloser struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (r *releasingReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.once.Do(r.release)
	return err
}

type releasingReadSeekCloser struct {
	io.ReadSeekCloser
	once    sync.Once
	release func()
}

func (r *releasingReadSeekCloser) Close() error {
	err := r.ReadSeekCloser.Close()
	r.once.Do(r.release)
	return err
}
