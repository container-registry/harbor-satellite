package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	auditlog "github.com/container-registry/harbor-satellite/internal/shared/logger"
)

var errTokenConsumed = errors.New("registration token already consumed")

// claimZTRToken claims token in one statement, so exactly one of several
// concurrent registrations wins. A missing token is fleet.ErrTokenInvalid and
// an already claimed one fleet.ErrTokenInvalid wrapping errTokenConsumed; Ztr
// answers both like an unknown token so the response does not reveal that the
// token is in use. An expired token is fleet.ErrTokenExpired.
func claimZTRToken(ctx context.Context, q *database.Queries, token string) (int32, error) {
	// expires_at is written with the app's clock (AddToken), so the claim
	// compares it with the same clock rather than the database's NOW().
	id, err := q.ClaimToken(ctx, database.ClaimTokenParams{Token: token, Now: time.Now()})
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("claim registration token: %w", err)
	}

	// The claim matched nothing. The token is unknown, already claimed, or,
	// since those are the only other conditions of the claim, expired.
	info, err := q.GetTokenByValue(ctx, token)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, fleet.ErrTokenInvalid
	case err != nil:
		return 0, fmt.Errorf("read registration token: %w", err)
	case info.ConsumedAt.Valid:
		return 0, fmt.Errorf("%w: %w", fleet.ErrTokenInvalid, errTokenConsumed)
	default:
		return 0, fleet.ErrTokenExpired
	}
}

// unclaimZTRToken undoes a claim after a later registration step failed, so
// the satellite can retry with the same token.
func unclaimZTRToken(ctx context.Context, q *database.Queries, token string) error {
	if err := q.UnclaimToken(ctx, token); err != nil {
		return fmt.Errorf("release registration token %s: %w", maskToken(token), err)
	}
	return nil
}

// rejectZTRToken audits and answers a failed claimZTRToken.
func (s *Server) rejectZTRToken(w http.ResponseWriter, r *http.Request, token string, err error) {
	masked := maskToken(token)
	reason := auditlog.ReasonInvalidToken
	appErr := &AppError{Message: "Error: Invalid Token", Code: http.StatusBadRequest}
	switch {
	case errors.Is(err, fleet.ErrTokenExpired):
		reason = auditlog.ReasonTokenExpired
		appErr = &AppError{Message: "Error: Token Expired", Code: http.StatusUnauthorized}
	case !errors.Is(err, fleet.ErrTokenInvalid):
		log.Printf("ZTR: token %s: %v", masked, err)
		HandleAppError(w, &AppError{Message: "Error: Zero-touch registration failed", Code: http.StatusInternalServerError})
		return
	}
	log.Printf("ZTR: rejected token %s: %v", masked, err)
	s.auditEvent(r, auditlog.AuditEvent{
		Operation:    auditlog.OpAuth,
		ResourceType: auditlog.ResSatellite,
		Outcome:      auditlog.OutcomeFailure,
		ActorType:    auditlog.ActorAnonymous,
		Reason:       reason,
		Details:      map[string]any{"masked_token": masked},
	})
	HandleAppError(w, appErr)
}
