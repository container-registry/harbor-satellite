package state

import (
	"context"
	"fmt"
	"slices"

	"github.com/container-registry/harbor-satellite/internal/satellite/store"
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

// withoutWanted(remove, wanted) drops from remove every entity whose image
// and digest an entity of wanted, the union of all groups' current entities,
// has. All groups replicate into one store, so one group must not delete
// what another group still wants (D12), and a registry store deletes by
// digest, which takes every tag of that digest with it. An entity without a
// digest is matched by image and tag.
func withoutWanted(remove, wanted []Entity) []Entity {
	kept := make([]Entity, 0, len(remove))
	for _, r := range remove {
		if !slices.ContainsFunc(wanted, func(w Entity) bool { return sameContent(r, w) }) {
			kept = append(kept, r)
		}
	}
	return kept
}

func sameContent(a, b Entity) bool {
	if a.Name != b.Name {
		return false
	}
	if a.Digest != "" {
		return a.Digest == b.Digest
	}
	return a.Tag == b.Tag
}
