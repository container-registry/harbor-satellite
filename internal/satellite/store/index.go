package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/telemetry"
	digest "github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content/oci"
)

// OCIStoreOptions are the plan 5.1 and 5.2 behaviours of OpenOCIStore; the
// zero value is NewOCIStore's behaviour.
type OCIStoreOptions struct {
	// AtomicIndex turns oras AutoSaveIndex off and persists index.json with
	// WriteIndexAtomic after every tag change. oras-go v2.6.2 rewrites it in
	// place with os.WriteFile (content/oci/oci.go:461-467), so a crash can
	// leave a torn file that stops the satellite from starting.
	AtomicIndex bool
	// RecoverIndex moves an index.json that oras cannot load aside and opens
	// an empty index instead of failing; OpenReport says so, and the caller
	// resyncs.
	RecoverIndex bool
	// FillLeases, when set, receives every descriptor a streaming fill writes
	// into the serving layout before the artifact is tagged, so
	// CollectGarbage keeps an in-flight fill.
	FillLeases *FillLeases
	// GCGrace is the grace period Delete passes to CollectGarbage. Zero
	// collects unreferenced content at once.
	GCGrace time.Duration
	// Recorder receives replication bytes and duration and index lock waits;
	// nil records nothing.
	Recorder telemetry.Recorder
}

type OpenReport struct {
	IndexRecovered  bool
	QuarantinedPath string
}

// OpenOCIStore is NewOCIStore with options.
func OpenOCIStore(root string, opts OCIStoreOptions) (*OCIStore, OpenReport, error) {
	if strings.TrimSpace(root) == "" {
		return nil, OpenReport{}, errors.New("OCI store root is required")
	}
	// A killed process cannot run the stage cleanup. This store is opened once,
	// before replication, so anything still under .stage belongs to that process.
	if err := clearStaleStages(root); err != nil {
		return nil, OpenReport{}, err
	}
	var report OpenReport
	target, err := openLayout(root, opts.AtomicIndex)
	if err != nil {
		if !opts.RecoverIndex || !indexExists(root) {
			return nil, report, fmt.Errorf("open OCI store at %s: %w", root, err)
		}
		quarantined, qErr := quarantineIndex(root)
		if qErr != nil {
			return nil, report, errors.Join(fmt.Errorf("open OCI store at %s: %w", root, err), qErr)
		}
		report = OpenReport{IndexRecovered: true, QuarantinedPath: quarantined}
		target, err = openLayout(root, opts.AtomicIndex)
		if err != nil {
			return nil, report, fmt.Errorf("open OCI store at %s after moving the index aside: %w", root, err)
		}
	}
	return &OCIStore{
		root:              root,
		store:             target,
		opts:              opts,
		readers:           NewRefCounter(),
		unreferencedSince: map[digest.Digest]time.Time{},
	}, report, nil
}

func openLayout(root string, atomicIndex bool) (*oci.Store, error) {
	target, err := oci.New(root)
	if err != nil {
		return nil, err
	}
	target.AutoSaveIndex = !atomicIndex
	return target, nil
}

func indexExists(root string) bool {
	_, err := os.Stat(filepath.Join(root, ocispec.ImageIndexFile))
	return err == nil
}

// quarantineIndex keeps the unreadable index for diagnosis. No last-good copy
// exists: atomic writes cannot tear, so a torn file comes from an older version.
func quarantineIndex(root string) (string, error) {
	src := filepath.Join(root, ocispec.ImageIndexFile)
	dst := fmt.Sprintf("%s.torn-%s", src, time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.Rename(src, dst); err != nil {
		return "", fmt.Errorf("move unreadable index aside: %w", err)
	}
	return dst, nil
}

// WriteIndexAtomic writes root/index.json through a temporary file in root,
// fsync, rename and a directory fsync.
func WriteIndexAtomic(root string, index ocispec.Index) (err error) {
	data, err := json.Marshal(index)
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}
	tmp, err := os.CreateTemp(root, ".index.json-*")
	if err != nil {
		return fmt.Errorf("create temporary index: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary index: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary index: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temporary index: %w", err)
	}
	if err = os.Chmod(tmp.Name(), 0o644); err != nil {
		return fmt.Errorf("set index permissions: %w", err)
	}
	if err = os.Rename(tmp.Name(), filepath.Join(root, ocispec.ImageIndexFile)); err != nil {
		return fmt.Errorf("replace index: %w", err)
	}
	return syncDir(root)
}

func syncDir(dir string) error {
	d, err := os.Open(filepath.Clean(dir))
	if err != nil {
		return fmt.Errorf("open %s for sync: %w", dir, err)
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return fmt.Errorf("sync %s: %w", dir, err)
	}
	return d.Close()
}

// BuildIndex renders the index from the store's tags, one descriptor per tag
// with its org.opencontainers.image.ref.name annotation.
func (s *OCIStore) BuildIndex(ctx context.Context) (ocispec.Index, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.buildIndexLocked(ctx)
}

func (s *OCIStore) buildIndexLocked(ctx context.Context) (ocispec.Index, error) {
	index := ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: []ocispec.Descriptor{},
	}
	var tags []string
	if err := s.store.Tags(ctx, "", func(page []string) error {
		tags = append(tags, page...)
		return nil
	}); err != nil {
		return ocispec.Index{}, err
	}
	for _, tag := range tags {
		desc, err := s.store.Resolve(ctx, tag)
		if err != nil {
			return ocispec.Index{}, fmt.Errorf("resolve tag %s: %w", tag, err)
		}
		annotations := make(map[string]string, len(desc.Annotations)+1)
		for k, v := range desc.Annotations {
			annotations[k] = v
		}
		annotations[ocispec.AnnotationRefName] = tag
		desc.Annotations = annotations
		index.Manifests = append(index.Manifests, desc)
	}
	return index, nil
}

// persistIndexLocked writes index.json after a tag change; the caller holds
// the exclusive index lock, so writes land in the order of the changes.
func (s *OCIStore) persistIndexLocked(ctx context.Context) error {
	if !s.opts.AtomicIndex {
		return nil
	}
	index, err := s.buildIndexLocked(ctx)
	if err != nil {
		return err
	}
	return WriteIndexAtomic(s.root, index)
}

// Check reports whether the layout's metadata files are readable; it backs
// the satellite's /healthz.
func (s *OCIStore) Check(_ context.Context) error {
	for _, name := range []string{ocispec.ImageLayoutFile, ocispec.ImageIndexFile} {
		f, err := os.Open(filepath.Join(s.root, name))
		if err != nil {
			return fmt.Errorf("OCI layout: %w", err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("OCI layout: %w", err)
		}
	}
	return nil
}
