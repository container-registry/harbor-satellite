// Package advisorylock is the one list of the PostgreSQL advisory lock keys
// Ground Control takes, so no two jobs share one. A job that must run once
// across replicas, such as the cleanup, takes a one-key ID and locks it with
// database.TryAdvisoryXactLock. Work on one aggregate is serialised with the
// two-key form, (namespace of the aggregate type, aggregate ID), which
// PostgreSQL keeps apart from the one-key IDs.
package advisorylock

const (
	// Cleanup serializes the status cleanup across replicas.
	Cleanup = 12345
)

// Namespaces of the two-key locks, one per aggregate type.
const (
	SatelliteAggregates = 1
	GroupAggregates     = 2
	ConfigAggregates    = 3
)

// IDs lists every one-key lock ID above by its constant's name.
func IDs() map[string]int64 {
	return map[string]int64{
		"Cleanup": Cleanup,
	}
}

// Namespaces lists every two-key namespace above by its constant's name.
func Namespaces() map[string]int32 {
	return map[string]int32{
		"SatelliteAggregates": SatelliteAggregates,
		"GroupAggregates":     GroupAggregates,
		"ConfigAggregates":    ConfigAggregates,
	}
}
