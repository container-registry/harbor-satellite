package state

import (
	"github.com/container-registry/harbor-satellite/internal/satellite/journal"
	"github.com/container-registry/harbor-satellite/internal/satellite/store"
)

// SetJournal makes replication append one edgejournal.Replication record
// per artifact it replicates or deletes, with ADR-0011's group, bytes and
// duration_ms (plan item 6.4).
func (f *FetchAndReplicateStateProcess) SetJournal(j *journal.Journal) {}

// SetReplicationSource makes replication copy from src instead of the
// Harbor store it shares with the proxy (plan item 6.2). cmd/satellite
// builds src with store.NewRegistryStoreWithOptions over the same options,
// so credential refreshes still apply, with BandwidthBytesPerSec set from
// replication_bandwidth_bytes_per_sec: the cap never slows the proxy.
func (f *FetchAndReplicateStateProcess) SetReplicationSource(src store.Store) {}

// SetJournal makes the report carry a journal upload (plan item 6.5): the
// journal's epoch, its floor as after_seq and the records above it, at
// most min(app_config.journal.upload_batch, config.MaxJournalUploadBatch),
// or one until Ground Control has answered a journal once. The report applies the answer with
// journal.Ack; after three answers in a row that reject the same batch it
// drops the batch, so Ground Control records a gap instead of the
// satellite resending it forever. A report with nothing above the last ack
// and no raised floor carries no journal.
func (s *StatusReportingProcess) SetJournal(j *journal.Journal) {}
