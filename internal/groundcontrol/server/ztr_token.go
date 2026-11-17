package server

import (
	"errors"
	"log"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	auditlog "github.com/container-registry/harbor-satellite/internal/shared/logger"
)

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
