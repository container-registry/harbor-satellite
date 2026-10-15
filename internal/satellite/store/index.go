package store

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// OCIStoreOptions are the plan 5.1 and 5.2 behaviours of OpenOCIStore; the
// zero value is NewOCIStore's behaviour.
type OCIStoreOptions struct {
	// AtomicIndex turns oras AutoSaveIndex off and persists index.json with
	// WriteIndexAtomic after every tag change. oras-go v2.6.2 rewrites it in
	// place with os.WriteFile (content/oci/oci.go:461-467), so a crash can
	// leave a torn file that stops the satellite from starting.
	AtomicIndex bool
	// RecoverIndex moves an undecodable index.json aside and opens an empty
	// index instead of failing; OpenReport says so, and the caller resyncs.
	RecoverIndex bool
	// FillLeases, when set, receives every descriptor Replicate pushes before
	// the artifact is tagged, so CollectGarbage keeps an in-flight copy.
	FillLeases *FillLeases
}

type OpenReport struct {
	IndexRecovered  bool
	QuarantinedPath string
}

// OpenOCIStore is NewOCIStore with options.
func OpenOCIStore(root string, opts OCIStoreOptions) (*OCIStore, OpenReport, error) {
	return nil, OpenReport{}, notimpl.Err("5.1 open OCI store with index recovery")
}

// WriteIndexAtomic writes root/index.json through a temporary file in root,
// fsync, rename and a directory fsync.
func WriteIndexAtomic(root string, index ocispec.Index) error {
	return notimpl.Err("5.1 atomic index write")
}

// BuildIndex renders the index from the store's tags, one descriptor per tag
// with its org.opencontainers.image.ref.name annotation.
func (s *OCIStore) BuildIndex(ctx context.Context) (ocispec.Index, error) {
	return ocispec.Index{}, notimpl.Err("5.1 build index from tags")
}
