package server

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestGroupStateCache(t *testing.T) {
	s := &Server{} // Nil map originally

	// Test lazy initialization and get/set
	states, ok := s.getCachedGroupStates(1)
	require.False(t, ok)
	require.Nil(t, states)

	s.setCachedGroupStates(1, []string{"state1", "state2"})
	states, ok = s.getCachedGroupStates(1)
	require.True(t, ok)
	require.Equal(t, []string{"state1", "state2"}, states)

	// Test invalidation
	s.invalidateCachedGroupStates(1)
	states, ok = s.getCachedGroupStates(1)
	require.False(t, ok)

	// Test clear
	s.setCachedGroupStates(1, []string{"state1"})
	s.setCachedGroupStates(2, []string{"state2"})
	s.clearGroupStateCache()

	states, ok = s.getCachedGroupStates(1)
	require.False(t, ok)
	states, ok = s.getCachedGroupStates(2)
	require.False(t, ok)
}

func TestGetOrFillGroupStates(t *testing.T) {
	server, mock := newMockServer(t)
	
	// Mock the DB queries on cache miss
	mock.ExpectQuery("SELECT .+ FROM satellite_groups WHERE satellite_id").
		WithArgs(int32(1)).
		WillReturnRows(sqlmock.NewRows([]string{"satellite_id", "group_id"}).AddRow(1, 10))
		
	mock.ExpectQuery("SELECT .+ FROM groups WHERE id").
		WithArgs(int32(10)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "group_name", "registry_url", "projects", "created_at", "updated_at"}).
			AddRow(10, "test-group", "http://harbor", nil, time.Now(), time.Now()))

	expectedStates := []string{"/satellite/group-state/test-group/state:latest"}

	// First call (cache miss) -> queries DB
	states, err := server.getOrFillGroupStates(context.Background(), 1, server.dbQueries)
	require.NoError(t, err)
	require.Equal(t, expectedStates, states)

	// Second call (cache hit) -> does NOT query DB (no new expectations needed, mock will fail if any queries are executed)
	states, err = server.getOrFillGroupStates(context.Background(), 1, server.dbQueries)
	require.NoError(t, err)
	require.Equal(t, expectedStates, states)

	// Invalidate cache
	server.invalidateCachedGroupStates(1)

	// Third call (cache miss after invalidation) -> queries DB again
	mock.ExpectQuery("SELECT .+ FROM satellite_groups WHERE satellite_id").
		WithArgs(int32(1)).
		WillReturnRows(sqlmock.NewRows([]string{"satellite_id", "group_id"}).AddRow(1, 10))
		
	mock.ExpectQuery("SELECT .+ FROM groups WHERE id").
		WithArgs(int32(10)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "group_name", "registry_url", "projects", "created_at", "updated_at"}).
			AddRow(10, "test-group", "http://harbor", nil, time.Now(), time.Now()))

	states, err = server.getOrFillGroupStates(context.Background(), 1, server.dbQueries)
	require.NoError(t, err)
	require.Equal(t, expectedStates, states)

	require.NoError(t, mock.ExpectationsWereMet())
}
