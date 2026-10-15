// Package edgejournal is the vocabulary of the satellite's edge journal
// (plan item 6.4, ADR-0015): the record types and their payloads. The
// satellite writes them and Ground Control turns them into edge events, so
// both import this package. Payload field names follow ADR-0011 (satellite
// eventing, open PR #597) where it defines them.
package edgejournal

// SchemaVersion is the payload version every record carries. Within a
// version fields are only added; a breaking change mints a new version.
const SchemaVersion = "1"

type RecordType string

const (
	// RecordReplication is one artifact replicated into, or deleted from,
	// the local store; its payload is a Replication.
	RecordReplication RecordType = "replication"
	// RecordCRI is the result of configuring one container runtime to pull
	// through the satellite; its payload is a CRI.
	RecordCRI RecordType = "cri"
	// RecordPullThrough is a proxy miss filled from Harbor; its payload is
	// a PullThrough.
	RecordPullThrough RecordType = "pull_through"
	// RecordDropped counts records the journal dropped under backpressure
	// instead of writing them; its payload is a Dropped. Dropped records
	// get no seq, so seqs stay contiguous.
	RecordDropped RecordType = "dropped"
)

// RecordTypes lists every RecordType.
func RecordTypes() []RecordType {
	return []RecordType{RecordReplication, RecordCRI, RecordPullThrough, RecordDropped}
}

type ReplicationAction string

const (
	ActionReplicated ReplicationAction = "replicated"
	ActionDeleted    ReplicationAction = "deleted"
)

// Replication is the payload of a RecordReplication. Error is set when the
// action failed.
type Replication struct {
	SchemaVersion string            `json:"schema_version"`
	CycleID       string            `json:"cycle_id,omitempty"`
	Action        ReplicationAction `json:"action"`
	Group         string            `json:"group"`
	Repository    string            `json:"repository"`
	Tag           string            `json:"tag"`
	Digest        string            `json:"digest"`
	Bytes         int64             `json:"bytes,omitempty"`
	DurationMs    int64             `json:"duration_ms,omitempty"`
	Error         string            `json:"error,omitempty"`
}

// CRI is the payload of a RecordCRI.
type CRI struct {
	SchemaVersion string `json:"schema_version"`
	Runtime       string `json:"runtime"`
	Success       bool   `json:"success"`
	BackupPath    string `json:"backup_path,omitempty"`
	Error         string `json:"error,omitempty"`
}

// PullThrough is the payload of a RecordPullThrough. Reference is the tag
// or digest the client asked for, Digest what it resolved to.
type PullThrough struct {
	SchemaVersion string `json:"schema_version"`
	Repository    string `json:"repository"`
	Reference     string `json:"reference"`
	Digest        string `json:"digest"`
	Bytes         int64  `json:"bytes,omitempty"`
	DurationMs    int64  `json:"duration_ms,omitempty"`
	Error         string `json:"error,omitempty"`
}

// Dropped is the payload of a RecordDropped.
type Dropped struct {
	SchemaVersion string `json:"schema_version"`
	Count         int64  `json:"count"`
}

// CloudEvents types of ADR-0011 that Ground Control reuses for the edge
// events it emits; edge events without an ADR-0011 counterpart use Ground
// Control's own naming (ADR-0015).
const (
	CloudEventArtifactSynchronized = "io.harborsatellite.artifact.synchronized"
	CloudEventArtifactDeleted      = "io.harborsatellite.artifact.deleted"
)
