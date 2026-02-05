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

// CreateJoinTokenRequest represents a request to generate a SPIRE join token
// without requiring satellite pre-registration.
type CreateJoinTokenRequest struct {
	SatelliteName string   `json:"satellite_name"`
	Region        string   `json:"region"`
	TTL           int      `json:"ttl_seconds,omitempty"` // Default: 600 (10 minutes)
	Selectors     []string `json:"selectors"`
}

// JoinTokenResponse contains the generated join token and bootstrap metadata.
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
		log.Printf("error: satellite with name '%s' already exists", req.SatelliteName)
		HandleAppError(w, &AppError{
			Message: "error: satellite already exists",
			Code:    http.StatusConflict,
		})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		log.Printf("error: failed to check satellite existence: %v", err)
		HandleAppError(w, &AppError{
			Message: "error: failed to check satellite",
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

	// Create satellite record in DB so admin can assign groups/configs before ZTR
	satellite, err := q.CreateSatellite(r.Context(), req.SatelliteName)
	if err != nil {
		log.Printf("Join token: Failed to create satellite record for %s: %v", req.SatelliteName, err)
		HandleAppError(w, &AppError{
			Message: "Failed to create satellite record",
			Code:    http.StatusInternalServerError,
		})
		return
	}
	log.Printf("Join token: Created satellite record for %s", req.SatelliteName)

	// Create robot account and link default config for new satellites
	if _, _, robotErr := ensureSatelliteRobotAccount(r, q, satellite); robotErr != nil {
		log.Printf("Join token: Failed to create robot account for %s: %v", req.SatelliteName, robotErr)
		HandleAppError(w, &AppError{
			Message: fmt.Sprintf("Failed to create robot account: %v", robotErr),
			Code:    http.StatusInternalServerError,
		})
		return
	}
	if configErr := ensureSatelliteConfig(r, q, satellite); configErr != nil {
		log.Printf("Join token: Failed to ensure config for %s: %v", req.SatelliteName, configErr)
		HandleAppError(w, &AppError{
			Message: fmt.Sprintf("Failed to ensure satellite config: %v", configErr),
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
