package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

// JoinTokenRequest represents a request to generate a SPIRE join token.
type JoinTokenRequest struct {
	Region string `json:"region"`
	TTL    int    `json:"ttl_seconds,omitempty"` // Default: 600 (10 minutes)
}

// CreateJoinTokenRequest represents a request to generate a SPIRE join token
// without requiring satellite pre-registration.
type CreateJoinTokenRequest struct {
	SatelliteName string `json:"satellite_name"`
	Region        string `json:"region"`
	TTL           int    `json:"ttl_seconds,omitempty"` // Default: 600 (10 minutes)
}

// JoinTokenResponse contains the generated join token and metadata.
type JoinTokenResponse struct {
	JoinToken string    `json:"join_token"`
	ExpiresAt time.Time `json:"expires_at"`
	SPIFFEID  string    `json:"spiffe_id"`
	Region    string    `json:"region"`
	Satellite string    `json:"satellite"`
}

// generateJoinTokenHandler generates a SPIRE join token for satellite bootstrap.
// POST /satellites/{name}/join-token
func (s *Server) generateJoinTokenHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	satelliteName := vars["satellite"]

	var req JoinTokenRequest
	if err := DecodeRequestBody(r, &req); err != nil {
		// If no body provided, use defaults
		req = JoinTokenRequest{
			Region: "default",
			TTL:    600, // 10 minutes
		}
	}

	if req.TTL <= 0 {
		req.TTL = 600 // Default 10 minutes
	}

	if req.TTL > 86400 {
		req.TTL = 86400 // Max 24 hours
	}

	if req.Region == "" {
		req.Region = "default"
	}

	// Verify satellite exists
	satellite, err := s.dbQueries.GetSatelliteByName(r.Context(), satelliteName)
	if err != nil {
		log.Printf("Join token: Satellite %s not found: %v", satelliteName, err)
		HandleAppError(w, &AppError{
			Message: "Error: Satellite not found",
			Code:    http.StatusNotFound,
		})
		return
	}

	// Generate a secure random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		log.Printf("Join token: Failed to generate token: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Failed to generate join token",
			Code:    http.StatusInternalServerError,
		})
		return
	}
	joinToken := hex.EncodeToString(tokenBytes)

	// Calculate expiry
	expiresAt := time.Now().Add(time.Duration(req.TTL) * time.Second)

	// Build SPIFFE ID for this satellite
	trustDomain := "harbor-satellite.local" // TODO: make configurable
	spiffeID := fmt.Sprintf("spiffe://%s/satellite/region/%s/%s",
		trustDomain, req.Region, satellite.Name)

	// In a real implementation, this would:
	// 1. Call SPIRE Server API to create the join token
	// 2. Create a registration entry for the satellite
	// For now, we store the token locally for validation

	// TODO: Integrate with SPIRE Server API
	// spireClient.CreateJoinToken(ctx, &api.JoinToken{
	//     Token: joinToken,
	//     Ttl:   int32(req.TTL),
	// })
	// spireClient.CreateEntry(ctx, &api.Entry{
	//     SpiffeId: spiffeID,
	//     ParentId: "spiffe://" + trustDomain + "/spire-agent",
	//     Selectors: []*api.Selector{
	//         {Type: "spiffe_id", Value: spiffeID},
	//     },
	// })

	log.Printf("Join token: Generated token for satellite %s (region: %s, expires: %v)",
		satellite.Name, req.Region, expiresAt)

	resp := JoinTokenResponse{
		JoinToken: joinToken,
		ExpiresAt: expiresAt,
		SPIFFEID:  spiffeID,
		Region:    req.Region,
		Satellite: satellite.Name,
	}

	WriteJSONResponse(w, http.StatusOK, resp)
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
		Enabled:   s.spiffeProvider != nil || s.embeddedSpire != nil,
		Connected: false,
	}

	if s.spiffeProvider != nil {
		status.TrustDomain = s.spiffeProvider.GetTrustDomain().String()
		status.Provider = "sidecar"
		status.Connected = true
	} else if s.embeddedSpire != nil {
		status.TrustDomain = s.embeddedSpire.GetTrustDomain()
		status.Provider = "embedded"
		status.Connected = s.embeddedSpire.GetClient() != nil
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

	// Check if embedded SPIRE is available
	if s.embeddedSpire == nil || s.embeddedSpire.GetClient() == nil {
		HandleAppError(w, &AppError{
			Message: "SPIRE server not configured",
			Code:    http.StatusServiceUnavailable,
		})
		return
	}

	client := s.embeddedSpire.GetClient()
	trustDomain := s.embeddedSpire.GetTrustDomain()

	// Build SPIFFE IDs
	agentSpiffeID := fmt.Sprintf("spiffe://%s/agent/%s", trustDomain, req.SatelliteName)
	workloadSpiffeID := fmt.Sprintf("spiffe://%s/satellite/region/%s/%s",
		trustDomain, req.Region, req.SatelliteName)

	// Generate join token via embedded SPIRE
	ttl := time.Duration(req.TTL) * time.Second
	joinToken, err := client.CreateJoinToken(r.Context(), agentSpiffeID, ttl)
	if err != nil {
		log.Printf("Failed to create join token: %v", err)
		HandleAppError(w, &AppError{
			Message: "Failed to create join token",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	// Create workload entry for the satellite (so it can get SVID after attestation)
	err = client.CreateWorkloadEntry(r.Context(), agentSpiffeID, workloadSpiffeID, []string{"unix:uid:0"})
	if err != nil {
		log.Printf("Failed to create workload entry: %v", err)
		// Continue - token is still valid, entry creation may succeed on retry
	}

	expiresAt := time.Now().Add(ttl)

	log.Printf("Join token: Generated token for satellite %s (region: %s, expires: %v)",
		req.SatelliteName, req.Region, expiresAt)

	resp := JoinTokenResponse{
		JoinToken: joinToken,
		ExpiresAt: expiresAt,
		SPIFFEID:  workloadSpiffeID,
		Region:    req.Region,
		Satellite: req.SatelliteName,
	}

	WriteJSONResponse(w, http.StatusOK, resp)
}
