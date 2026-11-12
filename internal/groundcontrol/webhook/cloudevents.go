package webhook

import (
	"encoding/json"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/edgejournal"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

const (
	ContentType   = "application/cloudevents+json"
	TypePrefix    = "io.harborsatellite.groundcontrol."
	SchemaBaseURL = "https://satellite.container-registry.com/schemas/events/v1/"
)

// adr0011Types are the edge kinds ADR-0011 defines a CloudEvents type for,
// as ADR-0015 maps them.
var adr0011Types = map[fleet.EventKind]string{ //nolint:exhaustive // only the kinds ADR-0011 defines a type for
	fleet.EventEdgeReplicated: edgejournal.CloudEventArtifactSynchronized,
	fleet.EventEdgeDeleted:    edgejournal.CloudEventArtifactDeleted,
}

// EventType is ADR-0011's type where it defines one, else TypePrefix + kind.
func EventType(kind fleet.EventKind) string {
	if t, ok := adr0011Types[kind]; ok {
		return t
	}
	return TypePrefix + string(kind)
}

type CloudEvent struct {
	SpecVersion     string          `json:"specversion"`
	ID              string          `json:"id"` // the outbox id: idempotency key
	Source          string          `json:"source"`
	Type            string          `json:"type"`
	Subject         string          `json:"subject,omitempty"`
	Time            time.Time       `json:"time"`
	DataContentType string          `json:"datacontenttype"`
	DataSchema      string          `json:"dataschema,omitempty"`
	Data            json.RawMessage `json:"data"`
}

// NewCloudEvent wraps a log entry: specversion 1.0, the outbox id as id,
// EventType, subject "<aggregate type>/<aggregate id>", the entry's time,
// application/json data and SchemaBaseURL + kind + ".json" as dataschema.
func NewCloudEvent(entry LogEntry, source string) (CloudEvent, error) {
	return CloudEvent{}, notimpl.Err("9.1 CloudEvent envelope")
}
