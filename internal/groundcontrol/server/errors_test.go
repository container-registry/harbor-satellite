package server

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

func TestStatusForError(t *testing.T) {
	pending.Plan(t, "1.4", "handlers map errors themselves; StatusForError is a stub")
	for _, tc := range []struct {
		err  error
		want int
	}{
		{fleet.ErrNotFound, http.StatusNotFound},
		{fleet.ErrAlreadyExists, http.StatusConflict},
		{fleet.ErrConflict, http.StatusConflict},
		{fleet.ErrInUse, http.StatusConflict},
		{fleet.ErrInvalid, http.StatusBadRequest},
		// A claimed token answers like an unknown one (D7), never 409.
		{fleet.ErrTokenInvalid, http.StatusBadRequest},
		{fleet.ErrTokenExpired, http.StatusUnauthorized},
		{fleet.ErrRevoked, http.StatusForbidden},
		{fleet.ErrForbidden, http.StatusForbidden},
		{fleet.ErrRegistryUnavailable, http.StatusBadGateway},
		{fleet.ErrNotProvisioned, http.StatusServiceUnavailable},
		{errors.New("boom"), http.StatusInternalServerError},
	} {
		require.Equal(t, tc.want, StatusForError(tc.err), "%v", tc.err)
		require.Equal(t, tc.want, StatusForError(fmt.Errorf("wrapped: %w", tc.err)), "wrapped %v", tc.err)
		require.Equal(t, tc.want, AppErrorFor(tc.err).Code, "%v", tc.err)
	}
}

func TestAppErrorFor_KeepsTodaysTokenBodies(t *testing.T) {
	pending.Plan(t, "1.4", "handlers map errors themselves; AppErrorFor is a stub")
	require.Equal(t, &AppError{Code: http.StatusBadRequest, Message: "Error: Invalid Token"}, AppErrorFor(fmt.Errorf("claim: %w", fleet.ErrTokenInvalid)))
	require.Equal(t, &AppError{Code: http.StatusUnauthorized, Message: "Error: Token Expired"}, AppErrorFor(fmt.Errorf("claim: %w", fleet.ErrTokenExpired)))
}
