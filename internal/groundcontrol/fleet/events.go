package fleet

import "context"

type EventKind string

const (
	EventSatelliteCreated  EventKind = "SatelliteCreated"
	EventSatelliteDeleted  EventKind = "SatelliteDeleted"
	EventGroupChanged      EventKind = "GroupChanged"
	EventGroupDeleted      EventKind = "GroupDeleted"
	EventConfigChanged     EventKind = "ConfigChanged"
	EventConfigDeleted     EventKind = "ConfigDeleted"
	EventAssignmentChanged EventKind = "AssignmentChanged"
)

// EventKinds lists every EventKind constant of this package.
func EventKinds() []EventKind {
	return []EventKind{
		EventSatelliteCreated,
		EventSatelliteDeleted,
		EventGroupChanged,
		EventGroupDeleted,
		EventConfigChanged,
		EventConfigDeleted,
		EventAssignmentChanged,
	}
}

type AggregateType string

const (
	AggregateSatellite AggregateType = "satellite"
	AggregateGroup     AggregateType = "group"
	AggregateConfig    AggregateType = "config"
	AggregatePolicy    AggregateType = "policy"
	// AggregateEdge is what a satellite reports about its own runtime,
	// such as pull-through and replication; the ID is the satellite's.
	AggregateEdge AggregateType = "edge"
)

// Aggregate is what an event is about. ID is the aggregate's row ID.
type Aggregate struct {
	Type AggregateType
	ID   int64
}

// Event is appended in the transaction that made the change. Payload is
// stored as JSON.
type Event struct {
	Kind      EventKind
	Aggregate Aggregate
	Payload   any
}

// Payloads are the public event catalogue (plan item 9): fields are added,
// never renamed. Every payload names its aggregate.

// ChangedPayload is the payload of every create and change event.
type ChangedPayload struct {
	Name string `json:"name"`
}

// Delete events carry what the publisher needs once the rows are gone.

// SatelliteDeletedPayload lists every robot of the satellite, because a
// rotation (plan item 8.4) can leave two for a while, and its SPIFFE ID
// so the tombstone also deletes the SPIRE entries.
type SatelliteDeletedPayload struct {
	Name           string      `json:"name"`
	RobotHarborIDs []int64     `json:"robot_harbor_ids"`
	SPIFFEID       string      `json:"spiffe_id,omitempty"`
	StateRef       ArtifactRef `json:"state_ref"`
}

type GroupDeletedPayload struct {
	Name     string        `json:"name"`
	Members  []SatelliteID `json:"members"`
	StateRef ArtifactRef   `json:"state_ref"`
}

type ConfigDeletedPayload struct {
	Name     string      `json:"name"`
	StateRef ArtifactRef `json:"state_ref"`
}

type EventSink interface {
	Append(ctx context.Context, events ...Event) error
}

// NopEventSink drops events; Tx.Events returns it until plan item 2.
type NopEventSink struct{}

func (NopEventSink) Append(context.Context, ...Event) error { return nil }

// AutocommitEvents is the event sink of Repository.Autocommit.
type AutocommitEvents struct{}

func (AutocommitEvents) Append(context.Context, ...Event) error { return ErrOutsideTransaction }
