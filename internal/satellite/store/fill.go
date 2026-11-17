package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
)

// maxManifestBytes is the manifest size the distribution spec asks
// registries to accept; a larger body is refused rather than buffered.
const maxManifestBytes = 4 << 20

// ErrUnknownSize means the upstream did not state the blob's size, so the
// stream cannot be verified before its last byte goes out.
var ErrUnknownSize = errors.New("blob size unknown")

// Filler is the streaming pull-through of plan 5.3, an optional capability
// next to Store.
type Filler interface {
	// FillManifest stores the manifest without its graph and returns it.
	FillManifest(ctx context.Context, source Store, artifact Artifact) (ocispec.Descriptor, []byte, error)
	// StreamBlob serves descriptor from source while writing it to the store;
	// the store commits the blob only after the digest verifies.
	StreamBlob(ctx context.Context, source Store, artifact Artifact, descriptor ocispec.Descriptor) (*http.Response, error)
	// CompleteGraph copies what is missing below root, then tags the artifact.
	CompleteGraph(ctx context.Context, source Store, artifact Artifact, root ocispec.Descriptor) error
}

var _ Filler = (*OCIStore)(nil)

// FillManifest resolves the artifact's manifest at source and stores its
// bytes untagged; a manifest already in the layout is read locally.
func (s *OCIStore) FillManifest(ctx context.Context, source Store, artifact Artifact) (ocispec.Descriptor, []byte, error) {
	desc, err := source.Pull(ctx, artifact, PullResourceManifest)
	if err != nil {
		return ocispec.Descriptor{}, nil, err
	}
	desc = ocispec.Descriptor{MediaType: desc.MediaType, Digest: desc.Digest, Size: desc.Size}
	if desc.Size > maxManifestBytes {
		return ocispec.Descriptor{}, nil, fmt.Errorf("manifest %s is %d bytes, more than %d", desc.Digest, desc.Size, maxManifestBytes)
	}
	if payload, fetchErr := content.FetchAll(ctx, s.layout(), desc); fetchErr == nil {
		return desc, payload, nil
	}
	response, err := source.Fetch(ctx, artifact, desc, PullResourceManifest, nil)
	if err != nil {
		return ocispec.Descriptor{}, nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ocispec.Descriptor{}, nil, fmt.Errorf("fetch manifest %s: upstream answered %s", desc.Digest, response.Status)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxManifestBytes+1))
	if err != nil {
		return ocispec.Descriptor{}, nil, fmt.Errorf("read manifest %s: %w", desc.Digest, err)
	}
	if int64(len(payload)) != desc.Size || digest.FromBytes(payload) != desc.Digest {
		return ocispec.Descriptor{}, nil, fmt.Errorf("manifest %s from upstream does not match its descriptor", desc.Digest)
	}
	lease := s.opts.FillLeases.Begin(artifact)
	defer lease.End()
	lease.Track(desc)
	if err := s.layout().Push(ctx, desc, bytes.NewReader(payload)); err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
		return ocispec.Descriptor{}, nil, fmt.Errorf("store manifest %s: %w", desc.Digest, err)
	}
	return desc, payload, nil
}

// StreamBlob answers a blob miss. The caller confirmed with source that the
// repository holds the blob; one upstream download per digest runs at a
// time, and a request that waited for it is served from the layout.
func (s *OCIStore) StreamBlob(ctx context.Context, source Store, artifact Artifact, descriptor ocispec.Descriptor) (*http.Response, error) {
	if descriptor.Size < 0 {
		return nil, ErrUnknownSize
	}
	for {
		if s.hasBlob(descriptor.Digest) {
			s.provenance.Store(provenanceKey(artifact.Name, descriptor.Digest), struct{}{})
			return s.Fetch(ctx, artifact, descriptor, PullResourceBlob, nil)
		}
		wait, leader := s.claimStream(descriptor.Digest)
		if !leader {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-wait:
			}
			continue
		}
		response, err := s.streamFromSource(ctx, source, artifact, descriptor)
		if err != nil {
			s.releaseStream(descriptor.Digest)
			return nil, err
		}
		return response, nil
	}
}

func (s *OCIStore) hasBlob(d digest.Digest) bool {
	if d.Validate() != nil {
		return false
	}
	_, err := os.Stat(blobFilePath(s.root, d))
	return err == nil
}

func blobFilePath(root string, d digest.Digest) string {
	return filepath.Join(root, ocispec.ImageBlobsDir, d.Algorithm().String(), d.Encoded())
}

func (s *OCIStore) claimStream(d digest.Digest) (wait <-chan struct{}, leader bool) {
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()
	if s.streams == nil {
		s.streams = map[digest.Digest]chan struct{}{}
	}
	if ch, ok := s.streams[d]; ok {
		return ch, false
	}
	s.streams[d] = make(chan struct{})
	return nil, true
}

func (s *OCIStore) releaseStream(d digest.Digest) {
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()
	if ch, ok := s.streams[d]; ok {
		close(ch)
		delete(s.streams, d)
	}
}

func (s *OCIStore) streamFromSource(ctx context.Context, source Store, artifact Artifact, descriptor ocispec.Descriptor) (*http.Response, error) {
	if err := descriptor.Digest.Validate(); err != nil {
		return nil, err
	}
	response, err := source.Fetch(ctx, artifact, descriptor, PullResourceBlob, nil)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, fmt.Errorf("fetch blob %s: upstream answered %s", descriptor.Digest, response.Status)
	}
	lease := s.opts.FillLeases.Begin(artifact)
	lease.Track(descriptor)
	ingest := filepath.Join(s.root, "ingest")
	var tmp *os.File
	if err := os.MkdirAll(ingest, 0o755); err == nil {
		tmp, _ = os.CreateTemp(ingest, descriptor.Digest.Encoded()+"_stream_*") //nolint:errcheck // without a file the stream is served but not kept
	}
	body := &verifyingTee{
		source:     response.Body,
		descriptor: descriptor,
		hasher:     descriptor.Digest.Algorithm().Hash(),
		file:       tmp,
		commit: func(path string) error {
			return commitBlobFile(path, blobFilePath(s.root, descriptor.Digest))
		},
		onCommit: func() {
			s.provenance.Store(provenanceKey(artifact.Name, descriptor.Digest), struct{}{})
		},
		done: func() {
			lease.End()
			s.releaseStream(descriptor.Digest)
		},
	}
	return contentResponse(descriptor, body), nil
}

// commitBlobFile publishes a verified ingest file the way oras does:
// read-only, then renamed into blobs/.
func commitBlobFile(tmp, dst string) error {
	if err := os.Chmod(tmp, 0o444); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

var errStreamMismatch = errors.New("upstream blob does not match its digest")

// verifyingTee passes the upstream body to the client and copies it into an
// ingest file. It holds back the last bytes until the digest verifies, so a
// corrupted body never reaches the client complete and is never committed.
type verifyingTee struct {
	source     io.ReadCloser
	descriptor ocispec.Descriptor
	hasher     hash.Hash
	file       *os.File
	commit     func(path string) error
	onCommit   func()
	done       func()

	read      int64
	finished  bool
	failed    error
	closeOnce sync.Once
}

func (t *verifyingTee) Read(p []byte) (int, error) {
	if t.failed != nil {
		return 0, t.failed
	}
	if t.finished {
		return 0, io.EOF
	}
	if remaining := t.descriptor.Size - t.read; int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := t.source.Read(p)
	t.read += int64(n)
	t.hasher.Write(p[:n])
	if t.file != nil && n > 0 {
		if _, werr := t.file.Write(p[:n]); werr != nil {
			t.dropFile()
		}
	}
	if t.read == t.descriptor.Size {
		if digest.NewDigest(t.descriptor.Digest.Algorithm(), t.hasher) != t.descriptor.Digest {
			t.failed = errStreamMismatch
			t.dropFile()
			return 0, t.failed
		}
		t.finished = true
		t.keepFile()
		return n, nil
	}
	if err == io.EOF {
		t.failed = io.ErrUnexpectedEOF
		t.dropFile()
		return n, t.failed
	}
	if err != nil {
		t.failed = err
		t.dropFile()
	}
	return n, err
}

func (t *verifyingTee) keepFile() {
	if t.file == nil {
		return
	}
	path := t.file.Name()
	syncErr := t.file.Sync()
	closeErr := t.file.Close()
	t.file = nil
	if syncErr != nil || closeErr != nil || t.commit(path) != nil {
		_ = os.Remove(path)
		return
	}
	t.onCommit()
}

func (t *verifyingTee) dropFile() {
	if t.file == nil {
		return
	}
	path := t.file.Name()
	_ = t.file.Close()
	_ = os.Remove(path)
	t.file = nil
}

func (t *verifyingTee) Close() error {
	err := t.source.Close()
	t.closeOnce.Do(func() {
		if !t.finished {
			t.dropFile()
		}
		t.done()
	})
	return err
}

// CompleteGraph copies what is missing below root through a stage, the way
// Replicate does, and tags the artifact only once the graph is complete.
// The lease keeps what the streaming fill already put into the layout.
func (s *OCIStore) CompleteGraph(ctx context.Context, source Store, artifact Artifact, root ocispec.Descriptor) error {
	lease := s.opts.FillLeases.Begin(artifact)
	defer lease.End()
	lease.Track(root)
	_ = walkDescriptorGraph(ctx, s.layout(), root, func(desc ocispec.Descriptor) error { //nolint:errcheck // a missing child only ends the walk; the copy fetches it
		lease.Track(desc)
		return nil
	})
	pinned := artifact
	pinned.Digest = root.Digest.String()
	if pinned.Tag == "" {
		pinned.Tag = pinned.Digest
	}
	return s.Replicate(ctx, source, []Artifact{pinned})
}
