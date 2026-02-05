package server

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
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

// CreateJoinTokenRequest represents a request to generate a SPIRE join token.
// Deprecated: Use RegisterSatelliteRequest with attestation_method="join_token" instead.
type CreateJoinTokenRequest struct {
	SatelliteName string   `json:"satellite_name"`
	Region        string   `json:"region"`
	TTL           int      `json:"ttl_seconds,omitempty"`
	Selectors     []string `json:"selectors"`
}

// JoinTokenResponse contains the generated join token and bootstrap metadata.
// Deprecated: Use RegisterSatelliteWithSPIFFEResponse instead.
type JoinTokenResponse struct {
	JoinToken          string    `json:"join_token"`
	ExpiresAt          time.Time `json:"expires_at"`
	SPIFFEID           string    `json:"spiffe_id"`
	Region             string    `json:"region"`
	Satellite          string    `json:"satellite"`
	SpireServerAddress string    `json:"spire_server_address"`
	SpireServerPort    int       `json:"spire_server_port"`
	TrustDomain        string    `json:"trust_domain"`
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

// createJoinTokenHandler generates a SPIRE join token for satellite bootstrap
// without requiring the satellite to be pre-registered.
// POST /join-tokens
func (s *Server) createJoinTokenHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateJoinTokenRequest
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

	if req.Region == "" {
		req.Region = "default"
	}

	if req.TTL <= 0 {
		req.TTL = 600 // 10 minutes default
	}

	if req.TTL > 86400 {
		req.TTL = 86400 // Max 24 hours
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
				Message: fmt.Sprintf("invalid selector format %q: must contain at least one ':'", sel),
				Code:    http.StatusBadRequest,
			})
			return
		}
	}

	if s.spireClient == nil {
		HandleAppError(w, &AppError{
			Message: "SPIRE server not configured",
			Code:    http.StatusServiceUnavailable,
		})
		return
	}

	// Check if satellite already exists
	q := s.dbQueries
	_, err := q.GetSatelliteByName(r.Context(), req.SatelliteName)
	if err == nil {
		log.Printf("satellite with name '%s' already exists", req.SatelliteName)
		HandleAppError(w, &AppError{
			Message: "satellite already exists",
			Code:    http.StatusConflict,
		})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		log.Printf("failed to check satellite existence: %v", err)
		HandleAppError(w, &AppError{
			Message: "failed to check satellite",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	trustDomain := s.spireTrustDomain

	// Build SPIFFE IDs
	agentSpiffeID := fmt.Sprintf("spiffe://%s/agent/%s", trustDomain, req.SatelliteName)
	workloadSpiffeID := fmt.Sprintf("spiffe://%s/satellite/region/%s/%s",
		trustDomain, req.Region, req.SatelliteName)

	// Generate join token via SPIRE server client
	ttl := time.Duration(req.TTL) * time.Second
	joinToken, err := s.spireClient.CreateJoinToken(r.Context(), agentSpiffeID, ttl)
	if err != nil {
		log.Printf("Failed to create join token: %v", err)
		HandleAppError(w, &AppError{
			Message: "Failed to create join token",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	// Create workload entry for the satellite (so it can get SVID after attestation)
	err = s.spireClient.CreateWorkloadEntry(r.Context(), agentSpiffeID, workloadSpiffeID, req.Selectors)
	if err != nil {
		log.Printf("Failed to create workload entry: %v", err)
		HandleAppError(w, &AppError{
			Message: "Failed to create workload entry",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	// Start transaction for database operations
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Printf("Join token: Failed to begin transaction: %v", err)
		HandleAppError(w, &AppError{
			Message: "Failed to begin transaction",
			Code:    http.StatusInternalServerError,
		})
		return
	}
	txQueries := q.WithTx(tx)

	// Create satellite record in DB so admin can assign groups/configs before ZTR
	satellite, err := txQueries.CreateSatellite(r.Context(), req.SatelliteName)
	if err != nil {
		_ = tx.Rollback()
		if _, dupErr := q.GetSatelliteByName(r.Context(), req.SatelliteName); dupErr == nil {
			log.Printf("satellite with name '%s' already exists (race condition)", req.SatelliteName)
			HandleAppError(w, &AppError{
				Message: "satellite already exists",
				Code:    http.StatusConflict,
			})
			return
		}
		log.Printf("Join token: Failed to create satellite record for %s: %v", req.SatelliteName, err)
		HandleAppError(w, &AppError{
			Message: "Failed to create satellite record",
			Code:    http.StatusInternalServerError,
		})
		return
	}
	log.Printf("Join token: Created satellite record for %s", req.SatelliteName)

	// Create robot account and link default config for new satellites
	if _, _, robotErr := ensureSatelliteRobotAccount(r, txQueries, satellite); robotErr != nil {
		_ = tx.Rollback()
		log.Printf("Join token: Failed to create robot account for %s: %v", req.SatelliteName, robotErr)
		HandleAppError(w, &AppError{
			Message: "Failed to create robot account",
			Code:    http.StatusInternalServerError,
		})
		return
	}
	if configErr := ensureSatelliteConfig(r, txQueries, satellite); configErr != nil {
		_ = tx.Rollback()
		log.Printf("Join token: Failed to ensure config for %s: %v", req.SatelliteName, configErr)
		HandleAppError(w, &AppError{
			Message: "Failed to ensure satellite config",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Join token: Failed to commit transaction for %s: %v", req.SatelliteName, err)
		HandleAppError(w, &AppError{
			Message: "Failed to commit satellite creation",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	expiresAt := time.Now().Add(ttl)

	log.Printf("Join token: Generated token for satellite %s (region: %s, expires: %v)",
		req.SatelliteName, req.Region, expiresAt)

	resp := JoinTokenResponse{
		JoinToken:          joinToken,
		ExpiresAt:          expiresAt,
		SPIFFEID:           workloadSpiffeID,
		Region:             req.Region,
		Satellite:          req.SatelliteName,
		SpireServerAddress: s.spireServerAddress,
		SpireServerPort:    s.spireServerPort,
		TrustDomain:        trustDomain,
	}

	WriteJSONResponse(w, http.StatusOK, resp)
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
		if req.ParentAgentID == "" {
			HandleAppError(w, &AppError{
				Message: "parent_agent_id is required for sshpop attestation",
				Code:    http.StatusBadRequest,
			})
			return
		}
		agentSpiffeID = req.ParentAgentID
	}

	workloadSpiffeID := fmt.Sprintf("spiffe://%s/satellite/region/%s/%s",
		trustDomain, req.Region, req.SatelliteName)

	err := s.spireClient.CreateWorkloadEntry(r.Context(), agentSpiffeID, workloadSpiffeID, req.Selectors)
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
		satellite, err = q.CreateSatellite(r.Context(), req.SatelliteName)
		if err != nil {
			log.Printf("Register: Failed to create satellite record for %s: %v", req.SatelliteName, err)
			HandleAppError(w, &AppError{
				Message: "Failed to create satellite record",
				Code:    http.StatusInternalServerError,
			})
			return
		}
		log.Printf("Register: Created satellite record for %s", req.SatelliteName)

		if _, _, robotErr := ensureSatelliteRobotAccount(r, q, satellite); robotErr != nil {
			log.Printf("Register: Failed to create robot account for %s: %v", req.SatelliteName, robotErr)
			HandleAppError(w, &AppError{
				Message: fmt.Sprintf("Failed to create robot account: %v", robotErr),
				Code:    http.StatusInternalServerError,
			})
			return
		}
		if configErr := ensureSatelliteConfig(r, q, satellite); configErr != nil {
			log.Printf("Register: Failed to ensure config for %s: %v", req.SatelliteName, configErr)
			HandleAppError(w, &AppError{
				Message: fmt.Sprintf("Failed to ensure satellite config: %v", configErr),
				Code:    http.StatusInternalServerError,
			})
			return
		}
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
