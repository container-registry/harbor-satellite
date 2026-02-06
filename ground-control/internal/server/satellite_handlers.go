package server

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
	"github.com/container-registry/harbor-satellite/ground-control/internal/spiffe"
	"github.com/container-registry/harbor-satellite/ground-control/internal/utils"
	"github.com/container-registry/harbor-satellite/ground-control/reg/harbor"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/gorilla/mux"
)

type SatelliteGroupParams struct {
	Satellite string `json:"satellite"`
	Group     string `json:"group"`
}

type RegisterSatelliteParams struct {
	Name       string    `json:"name"`
	Groups     *[]string `json:"groups,omitempty"`
	ConfigName string    `json:"config_name"`
}

type RegisterSatelliteResponse struct {
	Token string `json:"token"`
}

type SatelliteStatusParams struct {
	Name                string    `json:"name"`
	Activity            string    `json:"activity"`
	StateReportInterval string    `json:"state_report_interval"`
	LatestStateDigest   string    `json:"latest_state_digest"`
	LatestConfigDigest  string    `json:"latest_config_digest"`
	MemoryUsedBytes     uint64    `json:"memory_used_bytes"`
	StorageUsedBytes    uint64    `json:"storage_used_bytes"`
	CPUPercent          float64   `json:"cpu_percent"`
	RequestCreatedTime  time.Time `json:"request_created_time"`
	LastSyncDurationMs  int64     `json:"last_sync_duration_ms"`
	ImageCount          int       `json:"image_count"`
}

func (s *Server) registerSatelliteHandler(w http.ResponseWriter, r *http.Request) {
	if s.spiffeProvider != nil || s.spireClient != nil {
		HandleAppError(w, &AppError{
			Message: "satellite registration via this endpoint is disabled when SPIFFE is enabled. Use POST /api/satellites/register instead",
			Code:    http.StatusBadRequest,
		})
		return
	}

	var req RegisterSatelliteParams
	if err := DecodeRequestBody(r, &req); err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	if !utils.IsValidName(req.Name) {
		err := &AppError{
			Message: fmt.Sprintf(invalidNameMessage, "satellite"),
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	if !utils.IsValidName(req.ConfigName) {
		HandleAppError(w, &AppError{
			Message: "invalid or empty config_name",
			Code:    http.StatusBadRequest,
		})
		return
	}

	// If the robot account is already present, we need to check if the robot account
	// permissions need to be updated.
	// i.e, check if the satellite is already connected to the groups in the request body.
	// if not, then update the robot account.
	roboPresent, err := harbor.IsRobotPresent(r.Context(), req.Name)
	if err != nil {
		log.Println(err)
		err := &AppError{
			Message: fmt.Sprintf("Error querying for robot account: %v", err.Error()),
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	if roboPresent {
		err := &AppError{
			Message: "Error: Robot Account name already present. Try with different name",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	// Start a new transaction
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}
	// Create a new Queries object bound to the transaction
	q := s.dbQueries.WithTx(tx)
	committed := false
	var robotID int64

	// Ensure proper transaction handling with defer
	defer func() {
		if !committed {
			// Cleanup robot account if transaction failed
			if robotID != 0 {
				if _, delErr := harbor.DeleteRobotAccount(r.Context(), robotID); delErr != nil {
					log.Printf("Warning: Failed to cleanup robot account: %v", delErr)
				}
			}
			if err := tx.Rollback(); err != nil {
				log.Printf("Error: Failed to rollback transaction: %v", err)
				HandleAppError(w, &AppError{
					Message: "Error: Failed to rollback transaction",
					Code:    http.StatusInternalServerError,
				})
			}
		}
	}()

	// Create satellite
	satellite, err := q.CreateSatellite(r.Context(), req.Name)
	if err != nil {
		log.Printf("Error creating satellite %s: %v", req.Name, err)
		err := &AppError{
			Message: "Error: failed to create satellite",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	groupStates, err := addSatelliteToGroups(r.Context(), q, req.Groups, satellite.ID)
	if err != nil {
		log.Println("Error adding satellite to groups:", err)
		HandleAppError(w, err)
		return
	}

	if err := ensureSatelliteProjectExists(r.Context()); err != nil {
		log.Println("Error ensuring satellite project exists:", err)
		HandleAppError(w, err)
		return
	}

	// Create Robot Account for Satellite
	projects := []string{"satellite"}
	rbt, err := utils.CreateRobotAccForSatellite(r.Context(), projects, satellite.Name)
	if err != nil {
		log.Printf("Error creating robot account for satellite %s: %v", satellite.Name, err)
		err := &AppError{
			Message: "Error: failed to create robot account",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}
	robotID = rbt.ID

	secretHash, err := hashRobotCredentials(rbt.Secret)
	if err != nil {
		log.Printf("Error hashing robot credentials: %v", err)
		HandleAppError(w, &AppError{Message: "Error: failed to hash robot credentials", Code: http.StatusInternalServerError})
		return
	}
	expiry := sql.NullTime{}
	if rbt.ExpiresAt > 0 {
		expiry = sql.NullTime{Time: time.Unix(rbt.ExpiresAt, 0), Valid: true}
	}
	if err := storeRobotAccountInDB(r.Context(), q, rbt.Name, secretHash, strconv.FormatInt(rbt.ID, 10), satellite.ID, expiry); err != nil {
		log.Println("Error storing robot account in DB:", err)
		HandleAppError(w, err)
		return
	}

	if err := assignPermissionsToRobot(r.Context(), q, req.Groups, rbt.ID); err != nil {
		log.Println("Error assigning permissions to robot:", err)
		HandleAppError(w, err)
		return
	}

	config, err := q.GetConfigByName(r.Context(), req.ConfigName)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	setSatelliteConfigParams := database.SetSatelliteConfigParams{
		SatelliteID: satellite.ID,
		ConfigID:    config.ID,
	}

	if err := q.SetSatelliteConfig(r.Context(), setSatelliteConfigParams); err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	// Create the satellite's state artifact
	err = utils.CreateOrUpdateSatStateArtifact(r.Context(), req.Name, groupStates, req.ConfigName)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	// Add token to DB with 24-hour expiry
	token, err := GenerateRandomToken(32)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	tokenExpiry := time.Now().Add(24 * time.Hour)
	tk, err := q.AddToken(r.Context(), database.AddTokenParams{
		SatelliteID: satellite.ID,
		Token:       token,
		ExpiresAt:   tokenExpiry,
	})
	if err != nil {
		log.Println("error in token")
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Commit failed: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Could not commit transaction",
			Code:    http.StatusInternalServerError,
		})
		return
	}
	committed = true

	resp := RegisterSatelliteResponse{
		Token: tk,
	}

	WriteJSONResponse(w, http.StatusOK, resp)
}

func (s *Server) ztrHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	token := vars["token"]

	q := s.dbQueries

	// Get full token info including expiry
	tokenInfo, err := q.GetTokenByValue(r.Context(), token)
	if err != nil {
		masked := maskToken(token)
		log.Printf("Invalid Satellite Token %s: %v", masked, err)
		HandleAppError(w, &AppError{
			Message: "Error: Invalid Token",
			Code:    http.StatusBadRequest,
		})
		return
	}

	// Validate token expiry
	if time.Now().After(tokenInfo.ExpiresAt) {
		masked := maskToken(token)
		log.Printf("Expired Satellite Token %s (expired at %v)", masked, tokenInfo.ExpiresAt)
		HandleAppError(w, &AppError{
			Message: "Error: Token Expired",
			Code:    http.StatusUnauthorized,
		})
		return
	}

	satelliteID := tokenInfo.SatelliteID

	robot, err := q.GetRobotAccBySatelliteID(r.Context(), satelliteID)
	if err != nil {
		log.Println("Robot Account Not Found")
		log.Println(err)
		err := &AppError{
			Message: "Error: Robot Account Not Found for Satellite",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	// Refresh robot secret via Harbor and update stored hash
	freshSecret, err := refreshRobotSecret(r, q, robot)
	if err != nil {
		log.Printf("Error refreshing robot secret: %v", err)
		HandleAppError(w, &AppError{Message: "Error: failed to refresh robot secret", Code: http.StatusInternalServerError})
		return
	}

	// groups attached to satellite
	groups, err := q.SatelliteGroupList(r.Context(), satelliteID)
	if err != nil {
		log.Printf("failed to list groups for satellite: %v, %v", satelliteID, err)
		log.Println(err)
		err := &AppError{
			Message: "Error: Satellite Groups List Failed",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	states, err := getGroupStates(r.Context(), groups, q)
	if err != nil {
		log.Println("Error retrieving group states:", err)
		HandleAppError(w, &AppError{
			Message: "Error: Get Group By ID Failed",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	satellite, err := q.GetSatellite(r.Context(), satelliteID)
	if err != nil {
		HandleAppError(w, &AppError{Message: "satellite not found", Code: http.StatusNotFound})
		return
	}

	configObject, err := fetchSatelliteConfig(r.Context(), s.dbQueries, satelliteID)
	if err != nil {
		log.Printf("Error: Failed to fetch Satellite config: %v", err)
		HandleAppError(w, err)
		return
	}

	// For sanity, create (update) the state artifact during the registration process as well.
	err = utils.CreateOrUpdateSatStateArtifact(r.Context(), satellite.Name, states, configObject.ConfigName)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	satelliteState := utils.AssembleSatelliteState(satellite.Name)

	result := config.StateConfig{
		StateURL: satelliteState,
		RegistryCredentials: config.RegistryCredentials{
			Username: robot.RobotName,
			Password: freshSecret,
			URL:      config.URL(os.Getenv("HARBOR_URL")),
		},
	}

	err = q.DeleteToken(r.Context(), token)
	if err != nil {
		log.Println("error deleting token")
		log.Println(err)
		err := &AppError{
			Message: "Error: Error deleting token",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

// spiffeZtrHandler handles Zero-Touch Registration using SPIFFE mTLS authentication.
// The satellite's identity is extracted from the TLS client certificate (SVID).
// This eliminates the need for single-use tokens.
func (s *Server) spiffeZtrHandler(w http.ResponseWriter, r *http.Request) {
	// Extract SPIFFE ID from the TLS connection
	satelliteName, ok := spiffe.GetSatelliteName(r.Context())
	if !ok {
		spiffeID, ok := spiffe.GetSPIFFEID(r.Context())
		if !ok {
			log.Println("SPIFFE ZTR: No SPIFFE identity in request")
			HandleAppError(w, &AppError{
				Message: "Error: SPIFFE authentication required",
				Code:    http.StatusUnauthorized,
			})
			return
		}

		var err error
		satelliteName, err = spiffe.ExtractSatelliteNameFromSPIFFEID(spiffeID)
		if err != nil {
			log.Printf("SPIFFE ZTR: Invalid SPIFFE ID format: %v", err)
			HandleAppError(w, &AppError{
				Message: "Error: Invalid SPIFFE ID format for satellite",
				Code:    http.StatusBadRequest,
			})
			return
		}
	}

	log.Printf("SPIFFE ZTR: Processing registration for satellite %s", satelliteName)

	q := s.dbQueries

	satellite, err := q.GetSatelliteByName(r.Context(), satelliteName)
	if err != nil {
		log.Printf("SPIFFE ZTR: Satellite %s not found (error: %v), auto-registering...", satelliteName, err)
		satellite, err = s.autoRegisterSatellite(r, satelliteName)
		if err != nil {
			log.Printf("SPIFFE ZTR: Failed to auto-register satellite %s: %v", satelliteName, err)
			HandleAppError(w, &AppError{
				Message: "Error: failed to auto-register satellite",
				Code:    http.StatusInternalServerError,
			})
			return
		}
		log.Printf("SPIFFE ZTR: Auto-registered satellite %s with ID %d", satelliteName, satellite.ID)
	} else {
		log.Printf("SPIFFE ZTR: Found existing satellite %s with ID %d", satelliteName, satellite.ID)
	}

	var freshSecret string
	robot, err := q.GetRobotAccBySatelliteID(r.Context(), satellite.ID)
	if err != nil {
		// Robot not found - create new one (backward compat for satellites created before join-token flow)
		log.Printf("SPIFFE ZTR: Robot account not found for satellite %s, creating...", satelliteName)
		var initialSecret string
		robot, _, initialSecret, err = ensureSatelliteRobotAccount(r, q, satellite)
		if err != nil {
			log.Printf("SPIFFE ZTR: Failed to create robot account for satellite %s: %v", satelliteName, err)
			HandleAppError(w, &AppError{
				Message: "Error: failed to create robot account",
				Code:    http.StatusInternalServerError,
			})
			return
		}
		freshSecret = initialSecret

		if err := ensureSatelliteConfig(r, q, satellite); err != nil {
			log.Printf("SPIFFE ZTR: Failed to ensure config for satellite %s: %v", satelliteName, err)
			HandleAppError(w, &AppError{
				Message: "Error: failed to ensure satellite config",
				Code:    http.StatusInternalServerError,
			})
			return
		}
	} else {
		// Robot exists - refresh its secret so the satellite gets a fresh credential
		freshSecret, err = refreshRobotSecret(r, q, robot)
		if err != nil {
			log.Printf("SPIFFE ZTR: Failed to refresh robot secret for satellite %s: %v", satelliteName, err)
			HandleAppError(w, &AppError{
				Message: "Error: failed to refresh robot secret",
				Code:    http.StatusInternalServerError,
			})
			return
		}
	}

	groups, err := q.SatelliteGroupList(r.Context(), satellite.ID)
	if err != nil {
		log.Printf("SPIFFE ZTR: Failed to list groups for satellite %s: %v", satelliteName, err)
		HandleAppError(w, &AppError{
			Message: "Error: Satellite Groups List Failed",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	states, err := getGroupStates(r.Context(), groups, q)
	if err != nil {
		log.Printf("SPIFFE ZTR: Error retrieving group states: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Get Group By ID Failed",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	// When Harbor is available, create state artifacts; otherwise just return credentials
	skipHarborCheck := os.Getenv("SKIP_HARBOR_HEALTH_CHECK") == "true"
	var satelliteState string

	if !skipHarborCheck {
		configObject, err := fetchSatelliteConfig(r.Context(), s.dbQueries, satellite.ID)
		if err != nil {
			log.Printf("SPIFFE ZTR: Failed to fetch satellite config: %v", err)
			HandleAppError(w, err)
			return
		}

		err = utils.CreateOrUpdateSatStateArtifact(r.Context(), satellite.Name, states, configObject.ConfigName)
		if err != nil {
			log.Printf("SPIFFE ZTR: Failed to create state artifact: %v", err)
			HandleAppError(w, err)
			return
		}
		satelliteState = utils.AssembleSatelliteState(satellite.Name)
	} else {
		// Use placeholder state URL when Harbor is not available
		satelliteState = "placeholder://spiffe-testing/" + satellite.Name
		log.Printf("SPIFFE ZTR: Harbor not available, using placeholder state for satellite %s", satelliteName)
	}

	harborURL := os.Getenv("HARBOR_URL")
	if harborURL == "" {
		harborURL = "http://placeholder-registry:5000"
	}

	result := config.StateConfig{
		StateURL: satelliteState,
		RegistryCredentials: config.RegistryCredentials{
			Username: robot.RobotName,
			Password: freshSecret,
			URL:      config.URL(harborURL),
		},
	}

	log.Printf("SPIFFE ZTR: Successfully registered satellite %s", satelliteName)
	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) listSatelliteHandler(w http.ResponseWriter, r *http.Request) {
	result, err := s.dbQueries.ListSatellites(r.Context())
	if err != nil {
		log.Printf("Error: Failed to List Satellites: %v", err)
		err := &AppError{
			Message: "Error: Failed to List Satellites",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) syncHandler(w http.ResponseWriter, r *http.Request) {
	var req SatelliteStatusParams
	if err := DecodeRequestBody(r, &req); err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	// Check SPIFFE identity first for dual auth
	var satelliteName string
	if name, ok := spiffe.GetSatelliteName(r.Context()); ok {
		satelliteName = name
	} else {
		satelliteName = req.Name
	}

	sat, err := s.dbQueries.GetSatelliteByName(r.Context(), satelliteName)
	if err != nil {
		log.Printf("Unknown satellite: %s", satelliteName)
		HandleAppError(w, &AppError{
			Message: "unknown satellite entity",
			Code:    http.StatusForbidden,
		})
		return
	}

	normalizedInterval, err := normalizeHeartbeatInterval(req.StateReportInterval)
	if err != nil {
		log.Printf("Invalid heartbeat interval %q: %v", req.StateReportInterval, err)
		HandleAppError(w, &AppError{Message: "invalid heartbeat interval format", Code: http.StatusBadRequest})
		return
	}

	_, err = s.dbQueries.InsertSatelliteStatus(r.Context(), database.InsertSatelliteStatusParams{
		SatelliteID:        sat.ID,
		Activity:           req.Activity,
		LatestStateDigest:  toNullString(req.LatestStateDigest),
		LatestConfigDigest: toNullString(req.LatestConfigDigest),
		CpuPercent:         toNullString(fmt.Sprintf("%.2f", req.CPUPercent)),
		MemoryUsedBytes:    toNullInt64(int64(req.MemoryUsedBytes)),
		StorageUsedBytes:   toNullInt64(int64(req.StorageUsedBytes)),
		LastSyncDurationMs: toNullInt64(req.LastSyncDurationMs),
		ImageCount:         toNullInt32(int32(req.ImageCount)),
		ReportedAt:         req.RequestCreatedTime,
	})
	if err != nil {
		log.Printf("Failed to insert status: %v", err)
		HandleAppError(w, &AppError{Message: "failed to save status", Code: http.StatusInternalServerError})
		return
	}

	err = s.dbQueries.UpdateSatelliteLastSeen(r.Context(), database.UpdateSatelliteLastSeenParams{
		ID:                sat.ID,
		HeartbeatInterval: toNullString(normalizedInterval),
	})
	if err != nil {
		log.Printf("Failed to update last_seen: %v", err)
		HandleAppError(w, &AppError{Message: "failed to update last_seen", Code: http.StatusInternalServerError})
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) getSatelliteStatusHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	satelliteName := vars["satellite"]

	sat, err := s.dbQueries.GetSatelliteByName(r.Context(), satelliteName)
	if err != nil {
		HandleAppError(w, &AppError{Message: "satellite not found", Code: http.StatusNotFound})
		return
	}

	status, err := s.dbQueries.GetLatestSatelliteStatus(r.Context(), sat.ID)
	if err != nil {
		HandleAppError(w, &AppError{Message: "no status available", Code: http.StatusNotFound})
		return
	}

	WriteJSONResponse(w, http.StatusOK, status)
}

func (s *Server) getActiveSatellitesHandler(w http.ResponseWriter, r *http.Request) {
	satellites, err := s.dbQueries.GetActiveSatellites(r.Context())
	if err != nil {
		log.Printf("Failed to get active satellites: %v", err)
		HandleAppError(w, &AppError{Message: "failed to get active satellites", Code: http.StatusInternalServerError})
		return
	}
	WriteJSONResponse(w, http.StatusOK, satellites)
}

func (s *Server) getStaleSatellitesHandler(w http.ResponseWriter, r *http.Request) {
	satellites, err := s.dbQueries.GetStaleSatellites(r.Context())
	if err != nil {
		log.Printf("Failed to get stale satellites: %v", err)
		HandleAppError(w, &AppError{Message: "failed to get stale satellites", Code: http.StatusInternalServerError})
		return
	}
	WriteJSONResponse(w, http.StatusOK, satellites)
}

// ensureSatelliteRobotAccount creates a Harbor robot account for the satellite and stores its hash in DB.
// Returns the created robot account, the Harbor robot ID (for cleanup on failure), and the transient secret.
func ensureSatelliteRobotAccount(r *http.Request, q *database.Queries, satellite database.Satellite) (database.RobotAccount, int64, string, error) {
	var robotName, robotSecret string
	var harborRobotID int64
	var expiry sql.NullTime
	skipHarborCheck := os.Getenv("SKIP_HARBOR_HEALTH_CHECK") == "true"

	if !skipHarborCheck {
		if err := ensureSatelliteProjectExists(r.Context()); err != nil {
			return database.RobotAccount{}, 0, "", fmt.Errorf("ensure satellite project: %w", err)
		}

		projects := []string{"satellite"}
		rbt, err := utils.CreateRobotAccForSatellite(r.Context(), projects, satellite.Name)
		if err != nil {
			return database.RobotAccount{}, 0, "", fmt.Errorf("create robot account: %w", err)
		}
		harborRobotID = rbt.ID
		robotName = rbt.Name
		robotSecret = rbt.Secret
		if rbt.ExpiresAt > 0 {
			expiry = sql.NullTime{Time: time.Unix(rbt.ExpiresAt, 0), Valid: true}
		}
	} else {
		// WARNING: SKIP_HARBOR_HEALTH_CHECK is for testing/development only.
		// In this mode, a hardcoded placeholder secret is used. DO NOT enable in production.
		log.Printf("SPIFFE ZTR: Harbor not available, using placeholder credentials for satellite %s", satellite.Name)
		robotName = "robot$satellite-" + satellite.Name
		robotSecret = "spiffe-auto-registered-placeholder-secret"
	}

	secretHash, err := hashRobotCredentials(robotSecret)
	if err != nil {
		return database.RobotAccount{}, 0, "", fmt.Errorf("hash robot credentials: %w", err)
	}
	robotParams := database.AddRobotAccountParams{
		RobotName:       robotName,
		RobotSecretHash: secretHash,
		RobotID:         strconv.FormatInt(harborRobotID, 10),
		SatelliteID:     satellite.ID,
		RobotExpiry:     expiry,
	}
	robot, err := q.AddRobotAccount(r.Context(), robotParams)
	if err != nil {
		return database.RobotAccount{}, harborRobotID, "", fmt.Errorf("store robot account: %w", err)
	}

	log.Printf("SPIFFE ZTR: Created robot account %s for satellite %s", robotName, satellite.Name)
	return robot, harborRobotID, robotSecret, nil
}

// refreshRobotSecret refreshes the Harbor robot account secret and updates the hash in DB.
// Returns the fresh plaintext secret for pass-through to the satellite.
func refreshRobotSecret(r *http.Request, q *database.Queries, robot database.RobotAccount) (string, error) {
	skipHarborCheck := os.Getenv("SKIP_HARBOR_HEALTH_CHECK") == "true"
	if skipHarborCheck {
		// WARNING: SKIP_HARBOR_HEALTH_CHECK is for testing/development only.
		// In this mode, a hardcoded placeholder secret is used. DO NOT enable in production.
		log.Printf("SPIFFE ZTR: Harbor not available, skipping robot secret refresh for %s", robot.RobotName)
		return "spiffe-auto-registered-placeholder-secret", nil
	}

	harborRobotID, err := strconv.ParseInt(robot.RobotID, 10, 64)
	if err != nil {
		return "", fmt.Errorf("parse robot ID: %w", err)
	}

	resp, err := harbor.RefreshRobotAccount(r.Context(), "", harborRobotID)
	if err != nil {
		return "", fmt.Errorf("refresh robot secret in Harbor: %w", err)
	}
	if resp.Payload == nil {
		return "", fmt.Errorf("harbor returned nil payload for robot %s", robot.RobotName)
	}

	newSecret := resp.Payload.Secret
	newHash, err := hashRobotCredentials(newSecret)
	if err != nil {
		return "", fmt.Errorf("hash refreshed secret: %w", err)
	}
	err = q.UpdateRobotAccount(r.Context(), database.UpdateRobotAccountParams{
		ID:              robot.ID,
		RobotName:       robot.RobotName,
		RobotSecretHash: newHash,
		RobotID:         robot.RobotID,
		RobotExpiry:     robot.RobotExpiry,
	})
	if err != nil {
		return "", fmt.Errorf("update robot hash in DB: %w", err)
	}

	log.Printf("SPIFFE ZTR: Refreshed robot secret for %s", robot.RobotName)
	return newSecret, nil
}

// ensureSatelliteConfig links the satellite to the default config if no config is assigned.
func ensureSatelliteConfig(r *http.Request, q *database.Queries, satellite database.Satellite) error {
	_, err := fetchSatelliteConfig(r.Context(), q, satellite.ID)
	if err == nil {
		return nil
	}

	defaultConfig, err := q.GetConfigByName(r.Context(), "default")
	if err != nil {
		defaultConfigJSON := []byte(`{
  "app_config": {
    "log_level": "info",
    "state_replication_interval": "@every 00h00m30s",
    "register_satellite_interval": "@every 00h00m05s",
    "heartbeat_interval": "@every 00h00m30s",
    "local_registry": {
      "url": "http://127.0.0.1:8585"
    }
  },
  "zot_config": {
    "distSpecVersion": "1.1.0",
    "storage": { "rootDirectory": "./zot" },
    "http": { "address": "0.0.0.0", "port": "8585" },
    "log": { "level": "info" }
  }
}`)
		defaultConfig, err = q.CreateConfig(r.Context(), database.CreateConfigParams{
			ConfigName:  "default",
			RegistryUrl: os.Getenv("HARBOR_URL"),
			Config:      defaultConfigJSON,
		})
		if err != nil {
			return fmt.Errorf("create default config: %w", err)
		}

		if pushErr := utils.CreateAndPushConfigStateArtifact(r.Context(), defaultConfigJSON, "default"); pushErr != nil {
			log.Printf("SPIFFE ZTR: Warning - failed to create config-state artifact: %v", pushErr)
		}
	}

	configLinkParams := database.SetSatelliteConfigParams{
		SatelliteID: satellite.ID,
		ConfigID:    defaultConfig.ID,
	}
	if err := q.SetSatelliteConfig(r.Context(), configLinkParams); err != nil {
		return fmt.Errorf("link satellite to config: %w", err)
	}

	log.Printf("SPIFFE ZTR: Linked satellite %s to default config", satellite.Name)
	return nil
}

// autoRegisterSatellite automatically creates a satellite entry during SPIFFE ZTR.
// This enables true Zero Touch Registration where satellites don't need to be pre-registered.
func (s *Server) autoRegisterSatellite(r *http.Request, name string) (database.Satellite, error) {
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return database.Satellite{}, fmt.Errorf("begin transaction: %w", err)
	}

	q := s.dbQueries.WithTx(tx)
	committed := false
	var harborRobotID int64

	defer func() {
		if !committed {
			if harborRobotID != 0 {
				if _, delErr := harbor.DeleteRobotAccount(r.Context(), harborRobotID); delErr != nil {
					log.Printf("Warning: Failed to cleanup robot account during auto-register: %v", delErr)
				}
			}
			if err := tx.Rollback(); err != nil {
				log.Printf("Error: Failed to rollback auto-register transaction: %v", err)
			}
		}
	}()

	satellite, err := q.CreateSatellite(r.Context(), name)
	if err != nil {
		return database.Satellite{}, fmt.Errorf("create satellite: %w", err)
	}

	_, harborRobotID, _, err = ensureSatelliteRobotAccount(r, q, satellite)
	if err != nil {
		return database.Satellite{}, err
	}

	if err := ensureSatelliteConfig(r, q, satellite); err != nil {
		return database.Satellite{}, err
	}

	if err := tx.Commit(); err != nil {
		return database.Satellite{}, fmt.Errorf("commit transaction: %w", err)
	}
	committed = true

	log.Printf("SPIFFE ZTR: Auto-registered satellite %s with ID %d", name, satellite.ID)
	return satellite, nil
}

func (s *Server) GetSatelliteByName(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	satellite := vars["satellite"]

	result, err := s.dbQueries.GetSatelliteByName(r.Context(), satellite)
	if err != nil {
		log.Printf("error: failed to get satellite: %v", err)
		err := &AppError{
			Message: "Error: Failed to Get Satellite",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

// The state artifact corresponding to the satellite must be deleted.
func (s *Server) DeleteSatelliteByName(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	satellite := vars["satellite"]

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Printf("Error starting transaction: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Failed to start database transaction",
			Code:    http.StatusInternalServerError,
		})
		return
	}
	q := s.dbQueries.WithTx(tx)

	committed := false
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil {
				log.Printf("Error: Failed to rollback transaction: %v", err)
				HandleAppError(w, &AppError{
					Message: "Error: Failed to rollback transaction",
					Code:    http.StatusInternalServerError,
				})
			}
		}
	}()

	sat, err := q.GetSatelliteByName(r.Context(), satellite)
	if err != nil {
		log.Printf("error: failed to get satellite by name: %v", err)
		err := &AppError{
			Message: "Error: Satellite Not Found",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}
	robotAcc, err := q.GetRobotAccBySatelliteID(r.Context(), sat.ID)
	if err != nil {
		log.Printf("error: robotAcc for satellite does not exist: %v", err)
		err := &AppError{
			Message: "Error: Failed to Delete Satellite",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	robotID, err := strconv.ParseInt(robotAcc.RobotID, 10, 64)
	if err != nil {
		log.Printf("error: Invalid robot ID: %v", err)
		err := &AppError{
			Message: "Error: Failed to Delete Satellite",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	err = q.DeleteSatelliteByName(r.Context(), satellite)
	if err != nil {
		log.Printf("error: failed to delete satellite: %v", err)
		err := &AppError{
			Message: "Error: Failed to Delete Satellite",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	_, err = harbor.DeleteRobotAccount(r.Context(), robotID)
	if err != nil {
		log.Printf("error: failed to delete robot account: %v", err)
		err := &AppError{
			Message: "Error: Failed to Delete Satellite",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	err = utils.DeleteArtifact(utils.ConstructHarborDeleteURL(sat.Name, "satellite"))
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Commit failed: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Could not commit transaction",
			Code:    http.StatusInternalServerError,
		})
		return
	}
	committed = true

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}

func (s *Server) addSatelliteToGroup(w http.ResponseWriter, r *http.Request) {
	var req SatelliteGroupParams

	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	// Validate satellite and group
	if !utils.IsValidName(req.Satellite) {
		HandleAppError(w, &AppError{
			Message: fmt.Sprintf(invalidNameMessage, "satellite"),
			Code:    http.StatusBadRequest,
		})
		return
	}

	// Get satellite by name
	sat, err := s.dbQueries.GetSatelliteByName(r.Context(), req.Satellite)
	if err != nil {
		log.Printf("Error: Satellite Not Found: %v", err)
		err := &AppError{
			Message: "Error: Satellite Not Found",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	// Get group by name
	grp, err := s.dbQueries.GetGroupByName(r.Context(), req.Group)
	if err != nil {
		log.Printf("Error: Group Not Found: %v", err)
		err := &AppError{
			Message: "Error: Group Not Found",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	// Check if satellite is already in the group
	alreadyInGroup, err := s.dbQueries.CheckSatelliteInGroup(r.Context(), database.CheckSatelliteInGroupParams{
		SatelliteID: int32(sat.ID),
		GroupID:     int32(grp.ID),
	})
	if err != nil {
		log.Printf("Error: Failed to check satellite in group %v", err)
		err := &AppError{
			Message: "Error: Failed to check satellite in group",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	if alreadyInGroup {
		log.Printf("Satellite %s is already in group %s, no changes needed", req.Satellite, req.Group)
		WriteJSONResponse(w, http.StatusOK, map[string]string{"message": "Satellite is already in the group"})
		return
	}

	// Start a transaction
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Printf("Error starting transaction: %v", err)
		err := &AppError{
			Message: "Error: Failed to start database transaction",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	// Create a new Queries object bound to the transaction
	q := s.dbQueries.WithTx(tx)

	committed := false
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil {
				log.Printf("Error: Failed to rollback transaction: %v", err)
				HandleAppError(w, &AppError{
					Message: "Error: Failed to rollback transaction",
					Code:    http.StatusInternalServerError,
				})
			}
		}
	}()

	// Add satellite to group
	params := database.AddSatelliteToGroupParams{
		SatelliteID: int32(sat.ID),
		GroupID:     int32(grp.ID),
	}

	err = q.AddSatelliteToGroup(r.Context(), params)
	if err != nil {
		log.Printf("Error: Failed to add satellite to group: %v", err)
		err := &AppError{
			Message: "Error: Failed to add satellite to group",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	// Get updated group list after adding the new group
	groupList, err := q.SatelliteGroupList(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed to get updated satellite group list: %v", err)
		err := &AppError{
			Message: "Error: Failed to get updated satellite group list",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	var projects []string
	var groupStates []string

	for _, group := range groupList {
		grp, err := s.dbQueries.GetGroupByID(r.Context(), group.GroupID)
		if err != nil {
			log.Printf("Error: Failed to get group by ID %d: %v", group.GroupID, err)
			err := &AppError{
				Message: "Error: Failed to get group details",
				Code:    http.StatusInternalServerError,
			}
			HandleAppError(w, err)
			return
		}
		projects = append(projects, grp.Projects...)
		groupStates = append(groupStates, utils.AssembleGroupState(grp.GroupName))
	}

	configObject, err := fetchSatelliteConfig(r.Context(), s.dbQueries, sat.ID)
	if err != nil {
		log.Printf("Error: Failed to fetch Satellite config: %v", err)
		HandleAppError(w, err)
		return
	}

	// Get robot account permissions
	robotAcc, err := s.dbQueries.GetRobotAccBySatelliteID(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed to get robot account for satellite: %v", err)
		err := &AppError{
			Message: "Error: Failed to get robot account for satellite",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	// Update robot account permissions
	_, err = utils.UpdateRobotProjects(r.Context(), projects, robotAcc.RobotID)
	if err != nil {
		log.Printf("Error: Failed to update robot account permissions: %v", err)
		err := &AppError{
			Message: "Error: Failed to update robot account permissions",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	// Update the state artifact to also track the new group state artifact
	err = utils.CreateOrUpdateSatStateArtifact(r.Context(), sat.Name, groupStates, configObject.ConfigName)
	if err != nil {
		log.Printf("Error: Failed to update satellite state artifact: %v", err)
		HandleAppError(w, err)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Commit failed: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Could not commit transaction",
			Code:    http.StatusInternalServerError,
		})
		return
	}
	committed = true

	WriteJSONResponse(w, http.StatusOK, map[string]string{"message": "Satellite successfully added to group"})
}

// If the satellite is removed from the group, the state artifact must be updated accordingly as well.
func (s *Server) removeSatelliteFromGroup(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	groupName := vars["group"]
	satelliteName := vars["satellite"]

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Printf("Error starting transaction: %v", err)
		err := &AppError{
			Message: "Error: Failed to start database transaction",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	// Create a new Queries object bound to the transaction
	q := s.dbQueries.WithTx(tx)

	committed := false
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil {
				log.Printf("Error: Failed to rollback transaction: %v", err)
				HandleAppError(w, &AppError{
					Message: "Error: Failed to rollback transaction",
					Code:    http.StatusInternalServerError,
				})
			}
		}
	}()

	sat, err := q.GetSatelliteByName(r.Context(), satelliteName)
	if err != nil {
		log.Printf("Error: Satellite Not Found: %v", err)
		err := &AppError{
			Message: "Error: Satellite Not Found",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	grp, err := q.GetGroupByName(r.Context(), groupName)
	if err != nil {
		log.Printf("Error: Group Not Found: %v", err)
		err := &AppError{
			Message: "Error: Group Not Found",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	params := database.RemoveSatelliteFromGroupParams{
		SatelliteID: int32(sat.ID),
		GroupID:     int32(grp.ID),
	}

	err = q.RemoveSatelliteFromGroup(r.Context(), params)
	if err != nil {
		log.Printf("error: failed to remove satellite from group: %v", err)
		err := &AppError{
			Message: "Error: Failed to Remove Satellite from Group",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	robotAcc, err := q.GetRobotAccBySatelliteID(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed to Add permission to robot account: %v", err)
		err := &AppError{
			Message: "Error: Failed to Add permission to robot account",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	groupList, err := q.SatelliteGroupList(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed: %v", err)
		err := &AppError{
			Message: "Error: Failed to refresh satellite group list",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	var projects []string
	var groupStates []string

	for _, group := range groupList {
		grp, err := q.GetGroupByID(r.Context(), group.GroupID)
		if err != nil {
			log.Printf("Error: Failed: %v", err)
			err := &AppError{
				Message: "Error: Failed to to refresh satellite group list",
				Code:    http.StatusInternalServerError,
			}
			HandleAppError(w, err)
			return
		}
		projects = append(projects, grp.Projects...)
		groupStates = append(groupStates, utils.AssembleGroupState(grp.GroupName))
	}

	// 1. We need the list of state artifacts for the groups that satellite belongs to
	// 2. Update the satellite state artifact accordingly

	_, err = utils.UpdateRobotProjects(r.Context(), projects, robotAcc.RobotID)
	if err != nil {
		log.Printf("Error: Failed to Add permission to robot account: %v", err)
		err := &AppError{
			Message: "Error: Failed to update robot account permissions",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	configObject, err := fetchSatelliteConfig(r.Context(), q, sat.ID)
	if err != nil {
		log.Printf("Error: Failed to fetch Satellite config: %v", err)
		HandleAppError(w, err)
		return
	}

	// Update the state artifact to also track the new group state artifact
	err = utils.CreateOrUpdateSatStateArtifact(r.Context(), sat.Name, groupStates, configObject.ConfigName)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Commit failed: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Could not commit transaction",
			Code:    http.StatusInternalServerError,
		})
		return
	}
	committed = true

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}
