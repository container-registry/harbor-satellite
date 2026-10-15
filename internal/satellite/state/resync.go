package state

import "github.com/container-registry/harbor-satellite/internal/shared/notimpl"

// ResetReplicatedEntities forgets the replicated entities of every group and
// persists that, so the next Execute replicates every group again. The
// satellite calls it when OpenOCIStore recovered a torn index: state.json
// then claims content whose tags are gone.
func (f *FetchAndReplicateStateProcess) ResetReplicatedEntities() error {
	return notimpl.Err("5.1 resync after index recovery")
}
