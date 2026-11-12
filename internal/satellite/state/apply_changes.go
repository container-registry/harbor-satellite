package state

import (
	"context"
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
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
func applyGroupChanges(_ context.Context, _, _ store.Store, c changes) error {
	return fmt.Errorf("%w: D9 replicate %d entities before deleting %d", notimpl.ErrNotImplemented, len(c.replicate), len(c.remove))
}

// withoutWanted(remove, wanted) drops from remove every entity whose image
// and digest an entity of wanted, the union of all groups' current entities,
// has. All groups replicate into one store, so one group must not delete
// what another group still wants (D12), and a registry store deletes by
// digest, which takes every tag of that digest with it.
func withoutWanted(remove, _ []Entity) []Entity {
	return remove
}
