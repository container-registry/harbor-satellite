package state

import (
	"context"
	"fmt"
	"slices"

	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/rs/zerolog"
)

// changes is the store work of one replication tick.
type changes struct {
	replicate []Entity
	remove    []Entity
}

// applyGroupChanges replicates c.replicate from the source store into the
// local one first and then deletes c.remove, so a moved tag never disappears
// and content the old and new digest share is not downloaded again. The
// local store's digest-guarded Delete collects what the removed entities
// leave unreferenced.
func applyGroupChanges(ctx context.Context, local, source store.Store, c changes) error {
	if err := local.Replicate(ctx, source, c.replicate); err != nil {
		return fmt.Errorf("replicate: %w", err)
	}
	if err := local.Delete(ctx, c.remove); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	return nil
}

type retiredEntity struct {
	group  string
	entity Entity
}

// retire records entities the group at url no longer wants. The caller
// holds f.mu.
func (f *FetchAndReplicateStateProcess) retire(url string, entities []Entity) {
	if len(entities) == 0 {
		return
	}
	f.retired = append(f.retired, StateMap{url: url, Entities: append([]Entity(nil), entities...)})
}

// deleteRetired deletes the retired entities no current group wants. It
// runs after every group of the tick has replicated, so the union of all
// groups' entities is known: all groups replicate into one store, and one
// group must not delete what another still wants (D12). A retired entity
// some group wants is dropped from the list; that group owns it now. A
// failed delete keeps the list for the next tick, also across a restart.
func (f *FetchAndReplicateStateProcess) deleteRetired(ctx context.Context, local store.Store, log *zerolog.Logger) error {
	f.mu.Lock()
	retired := f.retired
	var wanted []Entity
	for i := range f.stateMap {
		wanted = append(wanted, f.stateMap[i].Entities...)
		wanted = append(wanted, f.stateMap[i].desired...)
	}
	f.mu.Unlock()
	if len(retired) == 0 {
		return nil
	}

	singleTags := store.DeletesSingleTags(local)
	var remove []retiredEntity
	var left []StateMap
	for _, g := range retired {
		entities := withoutWanted(g.Entities, wanted, singleTags)
		if len(entities) == 0 {
			continue
		}
		left = append(left, StateMap{url: g.url, Entities: entities})
		for _, e := range entities {
			remove = append(remove, retiredEntity{group: groupName(g.url), entity: e})
		}
	}
	f.LogChanges(entitiesOf(remove), nil, log)

	var deleteErr error
	if len(remove) > 0 {
		deleteErr = f.deleteJournaled(ctx, local, remove)
	}
	if deleteErr == nil {
		left = nil
		if f.directDeliverer != nil {
			if err := f.directDeliverer.Delete(ctx, entitiesOf(remove)); err != nil {
				log.Warn().Err(err).Msg("Direct delivery: failed to remove old tarballs")
			}
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	// Groups of a cancelled earlier tick may have retired more meanwhile.
	f.retired = append(left, f.retired[len(retired):]...)
	if f.stateFilePath != "" {
		if err := f.saveState(); err != nil {
			log.Warn().Err(err).Msg("Failed to persist state after deleting retired content")
		}
	}
	if deleteErr != nil {
		return fmt.Errorf("delete content no group wants: %w", deleteErr)
	}
	return nil
}

func entitiesOf(retired []retiredEntity) []Entity {
	out := make([]Entity, 0, len(retired))
	for _, r := range retired {
		out = append(out, r.entity)
	}
	return out
}

// withoutWanted(remove, wanted, singleTags) drops from remove every entity
// wanted, the union of all groups' current entities, still needs. A store
// that deletes single tags keeps the digest's other tags, so only the same
// image, tag and digest is still needed; keeping more would leave a dropped
// tag pullable. A store that deletes by digest takes every tag of that
// digest with it, so the same image and digest is still needed. An entity
// without a digest is matched by image and tag.
func withoutWanted(remove, wanted []Entity, singleTags bool) []Entity {
	same := sameContent
	if singleTags {
		same = sameTag
	}
	kept := make([]Entity, 0, len(remove))
	for _, r := range remove {
		if !slices.ContainsFunc(wanted, func(w Entity) bool { return same(r, w) }) {
			kept = append(kept, r)
		}
	}
	return kept
}

func sameContent(a, b Entity) bool {
	if a.Repository != b.Repository || a.Name != b.Name {
		return false
	}
	if a.Digest != "" {
		return a.Digest == b.Digest
	}
	return a.Tag == b.Tag
}

func sameTag(a, b Entity) bool {
	if a.Repository != b.Repository || a.Name != b.Name || a.Tag != b.Tag {
		return false
	}
	return a.Digest == "" || a.Digest == b.Digest
}
