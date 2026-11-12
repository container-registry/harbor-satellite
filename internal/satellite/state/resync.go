package state

import "fmt"

// ResetReplicatedEntities forgets the replicated entities of every group and
// persists that, so the next Execute replicates every group again. The
// satellite calls it when OpenOCIStore recovered a torn index: state.json
// then claims content whose tags are gone.
func (f *FetchAndReplicateStateProcess) ResetReplicatedEntities() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.stateMap {
		f.stateMap[i].Entities = nil
	}
	if f.stateFilePath == "" {
		return nil
	}
	if err := SaveState(f.stateFilePath, f.stateMap, f.currentConfigDigest); err != nil {
		return fmt.Errorf("persist the reset state: %w", err)
	}
	return nil
}
