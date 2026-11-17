package state

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/journal"
	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/internal/shared/edgejournal"
	"github.com/container-registry/harbor-satellite/internal/shared/logger"
)

// SetJournal makes replication append one edgejournal.Replication record
// per artifact it replicates or deletes, with ADR-0011's group, bytes and
// duration_ms (plan item 6.4).
func (f *FetchAndReplicateStateProcess) SetJournal(j *journal.Journal) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.edgeJournal = j
}

// SetReplicationSource makes replication copy from src instead of the
// Harbor store it shares with the proxy (plan item 6.2). cmd/satellite
// builds src with store.NewRegistryStoreWithOptions over the same options,
// so credential refreshes still apply, with BandwidthBytesPerSec set from
// replication_bandwidth_bytes_per_sec: the cap never slows the proxy.
func (f *FetchAndReplicateStateProcess) SetReplicationSource(src store.Store) {
	f.replicationSource = src
}

// startCycle prepares one Execute: a fresh cycle ID for the journal and
// the replication slots of replication_concurrency.
func (f *FetchAndReplicateStateProcess) startCycle() {
	var id [8]byte
	cycle := ""
	if _, err := rand.Read(id[:]); err == nil {
		cycle = hex.EncodeToString(id[:])
	}
	var slots chan struct{}
	if n := f.cm.GetReplicationConcurrency(); n > 0 {
		slots = make(chan struct{}, n)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cycleID = cycle
	f.replicationSlots = slots
}

func (f *FetchAndReplicateStateProcess) acquireSlot(ctx context.Context) (func(), error) {
	f.mu.Lock()
	slots := f.replicationSlots
	f.mu.Unlock()
	if slots == nil {
		return func() {}, nil
	}
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *FetchAndReplicateStateProcess) appliedDigest(index int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stateMap[index].appliedDigest
}

// deleteJournaled deletes entities from local in one call and journals
// each under the group that retired it.
func (f *FetchAndReplicateStateProcess) deleteJournaled(ctx context.Context, local store.Store, entities []retiredEntity) error {
	artifacts := make([]store.Artifact, 0, len(entities))
	for _, e := range entities {
		artifacts = append(artifacts, e.entity)
	}
	started := time.Now()
	err := local.Delete(ctx, artifacts)
	for _, e := range entities {
		f.journalReplication(ctx, e.group, edgejournal.ActionDeleted, e.entity, time.Since(started), err)
	}
	return err
}

// replicateBounded replicates artifacts in one replication slot.
func (f *FetchAndReplicateStateProcess) replicateBounded(ctx context.Context, group string, local, source store.Store, artifacts []Entity) error {
	release, err := f.acquireSlot(ctx)
	if err != nil {
		return err
	}
	defer release()
	return f.replicateEach(ctx, group, local, source, artifacts)
}

// replicateEach replicates one artifact per call, so one failing artifact
// does not hide the others, and journals each result.
func (f *FetchAndReplicateStateProcess) replicateEach(ctx context.Context, group string, local, source store.Store, artifacts []Entity) error {
	var errs []error
	for _, a := range artifacts {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		started := time.Now()
		err := local.Replicate(ctx, source, []store.Artifact{a})
		f.journalReplication(ctx, group, edgejournal.ActionReplicated, a, time.Since(started), err)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (f *FetchAndReplicateStateProcess) journalReplication(ctx context.Context, group string, action edgejournal.ReplicationAction, e Entity, took time.Duration, err error) {
	f.mu.Lock()
	j, cycle := f.edgeJournal, f.cycleID
	f.mu.Unlock()
	if j == nil {
		return
	}
	log := logger.FromContext(ctx)
	payload := edgejournal.Replication{
		SchemaVersion: edgejournal.SchemaVersion,
		CycleID:       cycle,
		Action:        action,
		Group:         group,
		Repository:    strings.Trim(strings.Trim(e.Repository, "/")+"/"+strings.Trim(e.Name, "/"), "/"),
		Tag:           e.Tag,
		Digest:        e.Digest,
		DurationMs:    took.Milliseconds(),
	}
	if err != nil {
		payload.Error = err.Error()
	}
	data, merr := json.Marshal(payload)
	if merr != nil {
		return
	}
	// Synced before the next artifact, so the journal holds each result
	// replication reached when the satellite dies.
	if _, err := j.Append(ctx, journal.Record{Type: edgejournal.RecordReplication, RecordedAt: time.Now().UTC(), Payload: data}); err != nil {
		log.Warn().Err(err).Str("group", group).Msg("Failed to journal replication result")
	}
}

// groupName is the group of a group state reference
// <registry>/satellite/group-state/<group>/state:<tag>, or the reference
// itself when it has another shape.
func groupName(ref string) string {
	const marker = "/group-state/"
	i := strings.Index(ref, marker)
	if i < 0 {
		return ref
	}
	name, _, _ := strings.Cut(ref[i+len(marker):], "/")
	return name
}

func (s *SatelliteState) clone() *SatelliteState {
	if s == nil {
		return nil
	}
	return &SatelliteState{States: append([]string(nil), s.States...), Config: s.Config}
}
