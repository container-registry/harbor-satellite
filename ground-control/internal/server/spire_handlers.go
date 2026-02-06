package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/ground-control/reg/harbor"
)

// RegisterSatelliteRequest represents a request to register a satellite with SPIFFE.
type RegisterSatelliteRequest struct {
	SatelliteName     string   `json:"satellite_name"`
	Region            string   `json:"region,omitempty"`
	Selectors         []string `json:"selectors"`
	AttestationMethod string   `json:"attestation_method"` // join_token, x509pop, sshpop
	TTLSeconds        int      `json:"ttl_seconds,omitempty"`
	ParentAgentID     string   `json:"parent_agent_id,omitempty"`
}

// RegisterSatelliteWithSPIFFEResponse contains satellite registration details.
type RegisterSatelliteWithSPIFFEResponse struct {
	Satellite          string     `json:"satellite"`
	Region             string     `json:"region"`
	SpiffeID           string     `json:"spiffe_id"`
	ParentAgentID      string     `json:"parent_agent_id,omitempty"`
	JoinToken          string     `json:"join_token,omitempty"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	SpireServerAddress string     `json:"spire_server_address"`
	SpireServerPort    int        `json:"spire_server_port"`
	TrustDomain        string     `json:"trust_domain"`
}

// AgentListResponse contains a list of attested agents.
type AgentListResponse struct {
	Agents []AgentInfoResponse `json:"agents"`
}

// AgentInfoResponse contains agent information for API response.
type AgentInfoResponse struct {
	SpiffeID        string    `json:"spiffe_id"`
	AttestationType string    `json:"attestation_type"`
	Selectors       []string  `json:"selectors,omitempty"`
	ExpiresAt       time.Time `json:"expires_at,omitempty"`
}

// SPIREStatusResponse contains SPIRE integration status.
type SPIREStatusResponse struct {
	Enabled     bool   `json:"enabled"`
	TrustDomain string `json:"trust_domain,omitempty"`
	Provider    string `json:"provider,omitempty"`
	Connected   bool   `json:"connected"`
}

// spireStatusHandler returns the status of SPIRE integration.
// GET /spire/status
func (s *Server) spireStatusHandler(w http.ResponseWriter, r *http.Request) {
	status := SPIREStatusResponse{
		Enabled:   s.spiffeProvider != nil || s.spireClient != nil,
		Connected: false,
	}

	if s.spiffeProvider != nil {
		status.TrustDomain = s.spiffeProvider.GetTrustDomain().String()
		status.Provider = "sidecar"
		status.Connected = true
	} else if s.embeddedSpire != nil {
		status.TrustDomain = s.spireTrustDomain
		status.Provider = "embedded"
		status.Connected = s.spireClient != nil
	} else if s.spireClient != nil {
		status.TrustDomain = s.spireTrustDomain
		status.Provider = "external"
		status.Connected = true
	}

	WriteJSONResponse(w, http.StatusOK, status)
}

// registerSatelliteWithSPIFFEHandler handles unified satellite registration for all attestation methods.
// POST /api/satellites/register
func (s *Server) registerSatelliteWithSPIFFEHandler(w http.ResponseWriter, r *http.Request) {
	var req RegisterSatelliteRequest
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, &AppError{
			Message: "Invalid request body",
			Code:    http.StatusBadRequest,
		})
		return
	}

	if req.SatelliteName == "" {
		HandleAppError(w, &AppError{
			Message: "satellite_name is required",
			Code:    http.StatusBadRequest,
		})
		return
	}

	if len(req.Selectors) == 0 {
		HandleAppError(w, &AppError{
			Message: "selectors is required",
			Code:    http.StatusBadRequest,
		})
		return
	}

	for _, sel := range req.Selectors {
		if !strings.Contains(sel, ":") {
			HandleAppError(w, &AppError{
				Message: fmt.Sprintf("invalid selector format %q: must contain ':'", sel),
				Code:    http.StatusBadRequest,
			})
			return
		}
	}

	validMethods := map[string]bool{"join_token": true, "x509pop": true, "sshpop": true}
	if !validMethods[req.AttestationMethod] {
		HandleAppError(w, &AppError{
			Message: "attestation_method must be one of: join_token, x509pop, sshpop",
			Code:    http.StatusBadRequest,
		})
		return
	}

	if req.AttestationMethod == "sshpop" && req.ParentAgentID == "" {
		HandleAppError(w, &AppError{
			Message: "parent_agent_id is required for sshpop attestation",
			Code:    http.StatusBadRequest,
		})
		return
	}

	if req.Region == "" {
		req.Region = "default"
	}

	if req.TTLSeconds <= 0 {
		req.TTLSeconds = 600
	}
	if req.TTLSeconds > 86400 {
		req.TTLSeconds = 86400
	}

	if s.spireClient == nil {
		HandleAppError(w, &AppError{
			Message: "SPIRE server not configured",
			Code:    http.StatusServiceUnavailable,
		})
		return
	}

	trustDomain := s.spireTrustDomain
	var agentSpiffeID string
	var joinToken string
	var expiresAt *time.Time

	switch req.AttestationMethod {
	case "join_token":
		agentSpiffeID = fmt.Sprintf("spiffe://%s/agent/%s", trustDomain, req.SatelliteName)

		ttl := time.Duration(req.TTLSeconds) * time.Second
		token, err := s.spireClient.CreateJoinToken(r.Context(), agentSpiffeID, ttl)
		if err != nil {
			log.Printf("Failed to create join token: %v", err)
			HandleAppError(w, &AppError{
				Message: "Failed to create join token",
				Code:    http.StatusInternalServerError,
			})
			return
		}
		joinToken = token
		exp := time.Now().Add(ttl)
		expiresAt = &exp

	case "x509pop":
		if req.ParentAgentID != "" {
			agentSpiffeID = req.ParentAgentID
		} else {
			agents, err := s.spireClient.ListAgents(r.Context(), "x509pop")
			if err != nil {
				log.Printf("Failed to list x509pop agents: %v", err)
				HandleAppError(w, &AppError{
					Message: "Failed to list agents",
					Code:    http.StatusInternalServerError,
				})
				return
			}

			expectedSelector := fmt.Sprintf("x509pop:subject:cn:%s", req.SatelliteName)
			for _, agent := range agents {
				for _, sel := range agent.Selectors {
					if sel == expectedSelector {
						agentSpiffeID = agent.SpiffeID
						break
					}
				}
				if agentSpiffeID != "" {
					break
				}
			}

			if agentSpiffeID == "" {
				HandleAppError(w, &AppError{
					Message: fmt.Sprintf("no x509pop agent found with selector %q", expectedSelector),
					Code:    http.StatusNotFound,
				})
				return
			}
		}

	case "sshpop":
		agentSpiffeID = req.ParentAgentID
	}

	workloadSpiffeID := fmt.Sprintf("spiffe://%s/satellite/region/%s/%s",
		trustDomain, req.Region, req.SatelliteName)

	workloadEntryID, err := s.spireClient.CreateWorkloadEntry(r.Context(), agentSpiffeID, workloadSpiffeID, req.Selectors)
	if err != nil {
		log.Printf("Failed to create workload entry: %v", err)
		HandleAppError(w, &AppError{
			Message: "Failed to create workload entry",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	q := s.dbQueries
	satellite, err := q.GetSatelliteByName(r.Context(), req.SatelliteName)
	if err != nil {
		// New satellite: wrap DB writes in a transaction with cleanup on failure
		// Use a detached context for cleanup so cancellation doesn't leave orphaned resources
		cleanupCtx := context.WithoutCancel(r.Context())

		tx, txErr := s.db.BeginTx(r.Context(), nil)
		if txErr != nil {
			log.Printf("Register: Failed to begin transaction: %v", txErr)
			if delErr := s.spireClient.DeleteWorkloadEntry(cleanupCtx, workloadEntryID); delErr != nil {
				log.Printf("Warning: Failed to cleanup workload entry: %v", delErr)
			}
			HandleAppError(w, &AppError{
				Message: "Failed to begin transaction",
				Code:    http.StatusInternalServerError,
			})
			return
		}

		txQueries := q.WithTx(tx)
		committed := false
		var harborRobotID int64

		// Cleanup contract: if any step below fails (CreateSatellite,
		// ensureSatelliteRobotAccount, ensureSatelliteConfig, or Commit),
		// this defer rolls back all previously created artifacts:
		//   1. SPIRE workload entry (always, via DeleteWorkloadEntry)
		//   2. Harbor robot account (if created, via DeleteRobotAccount)
		//   3. DB transaction (via Rollback)
		// Uses cleanupCtx so cleanup completes even if the request is cancelled.
		// Same pattern as autoRegisterSatellite.
		defer func() {
			if !committed {
				if delErr := s.spireClient.DeleteWorkloadEntry(cleanupCtx, workloadEntryID); delErr != nil {
					log.Printf("Warning: Failed to cleanup workload entry: %v", delErr)
				}
				if harborRobotID != 0 {
					if _, delErr := harbor.DeleteRobotAccount(cleanupCtx, harborRobotID); delErr != nil {
						log.Printf("Warning: Failed to cleanup robot account: %v", delErr)
					}
				}
				if rbErr := tx.Rollback(); rbErr != nil {
					log.Printf("Error: Failed to rollback transaction: %v", rbErr)
				}
			}
		}()

		satellite, err = txQueries.CreateSatellite(r.Context(), req.SatelliteName)
		if err != nil {
			log.Printf("Register: Failed to create satellite record for %s: %v", req.SatelliteName, err)
			HandleAppError(w, &AppError{
				Message: "Failed to create satellite record",
				Code:    http.StatusInternalServerError,
			})
			return
		}
		log.Printf("Register: Created satellite record for %s", req.SatelliteName)

		_, harborRobotID, err = ensureSatelliteRobotAccount(r, txQueries, satellite)
		if err != nil {
			log.Printf("Register: Failed to create robot account for %s: %v", req.SatelliteName, err)
			HandleAppError(w, &AppError{
				Message: fmt.Sprintf("Failed to create robot account: %v", err),
				Code:    http.StatusInternalServerError,
			})
			return
		}
		if configErr := ensureSatelliteConfig(r, txQueries, satellite); configErr != nil {
			log.Printf("Register: Failed to ensure config for %s: %v", req.SatelliteName, configErr)
			HandleAppError(w, &AppError{
				Message: fmt.Sprintf("Failed to ensure satellite config: %v", configErr),
				Code:    http.StatusInternalServerError,
			})
			return
		}

		if commitErr := tx.Commit(); commitErr != nil {
			log.Printf("Register: Failed to commit transaction for %s: %v", req.SatelliteName, commitErr)
			HandleAppError(w, &AppError{
				Message: "Failed to commit satellite creation",
				Code:    http.StatusInternalServerError,
			})
			return
		}
		committed = true
	} else {
		log.Printf("Register: Satellite %s already exists", req.SatelliteName)
	}

	log.Printf("Register: Registered satellite %s (method: %s, region: %s, agent: %s)",
		req.SatelliteName, req.AttestationMethod, req.Region, agentSpiffeID)

	resp := RegisterSatelliteWithSPIFFEResponse{
		Satellite:          req.SatelliteName,
		Region:             req.Region,
		SpiffeID:           workloadSpiffeID,
		ParentAgentID:      agentSpiffeID,
		JoinToken:          joinToken,
		ExpiresAt:          expiresAt,
		SpireServerAddress: s.spireServerAddress,
		SpireServerPort:    s.spireServerPort,
		TrustDomain:        trustDomain,
	}

	WriteJSONResponse(w, http.StatusOK, resp)
}

// listSpireAgentsHandler lists attested SPIRE agents.
// GET /api/spire/agents?attestation_type=x509pop
func (s *Server) listSpireAgentsHandler(w http.ResponseWriter, r *http.Request) {
	if s.spireClient == nil {
		HandleAppError(w, &AppError{
			Message: "SPIRE server not configured",
			Code:    http.StatusServiceUnavailable,
		})
		return
	}

	attestationType := r.URL.Query().Get("attestation_type")

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
		agentResponses = append(agentResponses, AgentInfoResponse{
			SpiffeID:        agent.SpiffeID,
			AttestationType: agent.AttestationType,
			Selectors:       agent.Selectors,
			ExpiresAt:       agent.ExpiresAt,
		})
	}

	WriteJSONResponse(w, http.StatusOK, AgentListResponse{Agents: agentResponses})
}
