package image

import (
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/internal/shared/edgejournal"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// PullThroughJournal records pull-through fills; the satellite's edge
// journal implements it with TryAppend, so a pull never waits for a sync.
type PullThroughJournal interface {
	RecordPullThrough(edgejournal.PullThrough)
}

// journalFill records a manifest filled from Harbor. Blob fills follow a
// manifest and would only repeat it.
func (p *pullHandler) journalFill(resource store.PullResource, artifact store.Artifact, reference string, desc ocispec.Descriptor, started time.Time, err error) {
	if p.journal == nil || resource != store.PullResourceManifest {
		return
	}
	record := edgejournal.PullThrough{
		SchemaVersion: edgejournal.SchemaVersion,
		Repository:    artifact.Name,
		Reference:     reference,
		Digest:        desc.Digest.String(),
		Bytes:         desc.Size,
		DurationMs:    time.Since(started).Milliseconds(),
	}
	if err != nil {
		record.Error = err.Error()
		record.Digest = ""
		record.Bytes = 0
	}
	p.journal.RecordPullThrough(record)
}
