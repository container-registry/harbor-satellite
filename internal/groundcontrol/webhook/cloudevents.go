package webhook

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/edgejournal"
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
	if source == "" {
		return CloudEvent{}, errors.New("a CloudEvent needs a source")
	}
	data := entry.Payload
	if len(data) == 0 {
		data = json.RawMessage("{}")
	}
	if !json.Valid(data) {
		return CloudEvent{}, fmt.Errorf("outbox event %d has a payload that is not JSON", entry.Position.ID)
	}
	return CloudEvent{
		SpecVersion:     "1.0",
		ID:              strconv.FormatInt(entry.Position.ID, 10),
		Source:          source,
		Type:            EventType(entry.Kind),
		Subject:         string(entry.Aggregate.Type) + "/" + strconv.FormatInt(entry.Aggregate.ID, 10),
		Time:            entry.CreatedAt,
		DataContentType: "application/json",
		DataSchema:      SchemaBaseURL + string(entry.Kind) + ".json",
		Data:            data,
	}, nil
}
