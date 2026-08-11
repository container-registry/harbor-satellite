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

	t.Run("store nil stores empty non-nil slice", func(t *testing.T) {
		s.setGroupStatesCache(satelliteID, nil)
		result := s.getGroupStatesFromCache(satelliteID)

		if result == nil {
			t.Error("expected non-nil empty slice after storing nil, got nil")
		}

		if len(result) != 0 {
			t.Errorf("expected 0 states, got %d", len(result))
		}
	})

	t.Run("set if absent sets when empty and does not overwrite", func(t *testing.T) {
		// Ensure the entry is absent regardless of prior subtests' state.
		s.invalidateGroupStatesCache(satelliteID)

		// Entry absent: set-if-absent writes the value.
		s.setGroupStatesCacheIfAbsent(satelliteID, testStates)
		result := s.getGroupStatesFromCache(satelliteID)
		if result == nil {
			t.Fatal("expected non-nil result after set-if-absent on empty cache")
		}

		// Entry present: set-if-absent must not overwrite it, as a cache fill
		// that raced with a group-state mutation must not clobber the mutation's
		// newer entry.
		s.setGroupStatesCacheIfAbsent(satelliteID, []string{"stale"})
		result = s.getGroupStatesFromCache(satelliteID)
		if len(result) != len(testStates) {
			t.Fatalf("expected existing %d states preserved, got %d", len(testStates), len(result))
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

		if result1 == nil {
			t.Fatal("satellite 1: expected non-nil result")
		}
		if len(result1) != len(states1) {
			t.Errorf("satellite 1: expected %d states, got %d", len(states1), len(result1))
		}
		for i, state := range states1 {
			if result1[i] != state {
				t.Errorf("satellite 1: state at index %d: expected %s, got %s", i, state, result1[i])
			}
		}

		if result2 == nil {
			t.Fatal("satellite 2: expected non-nil result")
		}
		if len(result2) != len(states2) {
			t.Errorf("satellite 2: expected %d states, got %d", len(states2), len(result2))
		}
		for i, state := range states2 {
			if result2[i] != state {
				t.Errorf("satellite 2: state at index %d: expected %s, got %s", i, state, result2[i])
			}
		}
	})
}
