package server

import (
	"testing"
)

func TestGroupStatesCache(t *testing.T) {
	s := &Server{
		groupStatesCache: make(map[int32][]string),
	}

	satelliteID := int32(42)
	testStates := []string{"state1", "state2", "state3"}

	t.Run("get empty cache returns nil", func(t *testing.T) {
		result := s.getGroupStatesFromCache(satelliteID)
		if result != nil {
			t.Errorf("expected nil for empty cache, got %v", result)
		}
	})

	t.Run("set and get cache", func(t *testing.T) {
		s.setGroupStatesCache(satelliteID, testStates)
		result := s.getGroupStatesFromCache(satelliteID)

		if result == nil {
			t.Fatal("expected non-nil result")
		}

		if len(result) != len(testStates) {
			t.Errorf("expected %d states, got %d", len(testStates), len(result))
		}

		for i, state := range testStates {
			if result[i] != state {
				t.Errorf("state at index %d: expected %s, got %s", i, state, result[i])
			}
		}
	})

	t.Run("invalidate cache", func(t *testing.T) {
		s.setGroupStatesCache(satelliteID, testStates)
		s.invalidateGroupStatesCache(satelliteID)
		result := s.getGroupStatesFromCache(satelliteID)

		if result != nil {
			t.Errorf("expected nil after invalidation, got %v", result)
		}
	})

	t.Run("multiple satellites", func(t *testing.T) {
		sat1 := int32(1)
		sat2 := int32(2)
		states1 := []string{"a", "b"}
		states2 := []string{"c", "d", "e"}

		s.setGroupStatesCache(sat1, states1)
		s.setGroupStatesCache(sat2, states2)

		result1 := s.getGroupStatesFromCache(sat1)
		result2 := s.getGroupStatesFromCache(sat2)

		if len(result1) != len(states1) {
			t.Errorf("satellite 1: expected %d states, got %d", len(states1), len(result1))
		}

		if len(result2) != len(states2) {
			t.Errorf("satellite 2: expected %d states, got %d", len(states2), len(result2))
		}
	})
}
