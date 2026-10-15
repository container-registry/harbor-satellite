package fleettest

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ fleet.EventSink = (*EventSink)(nil)

// EventSink records appended events as "Events.Append" calls whose Args are
// the events.
type EventSink struct {
	*Recorder
}

// NewEventSink records into rec, or into a new Recorder when rec is nil.
func NewEventSink(rec *Recorder) *EventSink {
	return &EventSink{Recorder: orNew(rec)}
}

func (s *EventSink) Append(_ context.Context, events ...fleet.Event) error {
	args := make([]any, 0, len(events))
	for _, e := range events {
		args = append(args, e)
	}
	return s.Record("Events.Append", args...)
}

func (s *EventSink) AppendToGroupMembers(_ context.Context, group fleet.GroupID, kind fleet.EventKind) error {
	return s.Record("Events.AppendToGroupMembers", group, kind)
}

// Appended returns the events of every recorded append, failed ones
// included, in order.
func (s *EventSink) Appended() []fleet.Event {
	var out []fleet.Event
	for _, c := range s.CallsTo("Events.Append") {
		for _, a := range c.Args {
			if e, ok := a.(fleet.Event); ok {
				out = append(out, e)
			}
		}
	}
	return out
}
