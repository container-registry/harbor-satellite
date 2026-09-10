package events

import (
	"testing"

	"github.com/rs/zerolog"
)

func TestNewEventScheduler(t *testing.T) {
	log := zerolog.Nop()

	s := NewEventScheduler(&log)

	if s == nil {
		t.Fatal("expected non-nil EventScheduler")
	}
	if s.log != &log {
		t.Error("expected log to be set to the provided logger")
	}
	if s.eventMap == nil {
		t.Error("expected eventMap to be initialized")
	}
	if len(s.eventMap) != 0 {
		t.Errorf("expected eventMap to start empty, got %d entries", len(s.eventMap))
	}
}
