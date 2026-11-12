package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestMapError(t *testing.T) {
	other := errors.New("connection reset")
	for _, tc := range []struct {
		err  error
		want error
	}{
		{sql.ErrNoRows, fleet.ErrNotFound},
		{fmt.Errorf("get satellite: %w", sql.ErrNoRows), fleet.ErrNotFound},
		{&pq.Error{Code: "23505"}, fleet.ErrAlreadyExists},
		{&pq.Error{Code: "23503"}, fleet.ErrInUse},
		{&pq.Error{Code: "23001"}, fleet.ErrInUse},
		{other, other},
	} {
		got := mapError(tc.err)
		require.ErrorIs(t, got, tc.want, "%v", tc.err)
		require.ErrorIs(t, got, tc.err, "the cause stays reachable")
	}
	require.NoError(t, mapError(nil))
}
