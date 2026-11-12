package server

import (
	"log"
	"net/http"
	"time"
)

// GetSpireStatus returns the status of SPIRE integration.
// GET /spire/status
func (s *Server) GetSpireStatus(w http.ResponseWriter, _ *http.Request) {
	enabled := s.spireEnabled
	status := SPIREStatusResponse{
		Enabled:   enabled,
		Connected: false,
	}

	if s.spiffeProvider != nil {
		trustDomain := s.spiffeProvider.GetTrustDomain().String()
		provider := "sidecar"
		status.Connected = true
		status.TrustDomain = trustDomain
		status.Provider = provider
	} else {
		switch {
		case s.embeddedSpire != nil:
			provider := "embedded"
			status.Connected = s.spireClient != nil
			status.TrustDomain = s.spireTrustDomain
			status.Provider = provider
		case s.spireClient != nil:
			provider := "external"
			status.Connected = true
			status.TrustDomain = s.spireTrustDomain
			status.Provider = provider
		}
	}

	WriteJSONResponse(w, http.StatusOK, status)
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// ListSpireAgents lists attested SPIRE agents.
// GET /api/spire/agents?attestation_type=x509pop
func (s *Server) ListSpireAgents(w http.ResponseWriter, r *http.Request, params ListSpireAgentsParams) {
	if s.spireClient == nil {
		HandleAppError(w, &AppError{
			Message: "SPIRE server not configured",
			Code:    http.StatusServiceUnavailable,
		})
		return
	}

	attestationType := ""
	if params.AttestationType != "" {
		attestationType = params.AttestationType
	}

	agents, err := s.spireClient.ListAgents(r.Context(), attestationType)
	if err != nil {
		log.Printf("Failed to list agents: %v", err)
		HandleAppError(w, &AppError{
			Message: "Failed to list agents",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	agentResponses := make([]AgentInfoResponse, 0, len(agents))
	for _, agent := range agents {
		var expiresAt *time.Time
		if !agent.ExpiresAt.IsZero() {
			expiresAt = &agent.ExpiresAt
		}
		agentResponses = append(agentResponses, AgentInfoResponse{
			SpiffeID:        agent.SpiffeID,
			AttestationType: agent.AttestationType,
			Selectors:       agent.Selectors,
			ExpiresAt:       expiresAt,
		})
	}

	WriteJSONResponse(w, http.StatusOK, AgentListResponse{Agents: agentResponses})
}
