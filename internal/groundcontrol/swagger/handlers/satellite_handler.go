package handlers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/crypto"
	"github.com/container-registry/harbor-satellite/internal/env"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbor"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	swaggermodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/models"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/satellites"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/utils"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
)

func DeleteSatellite(params satellites.DeleteSatelliteParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return satellites.NewDeleteSatelliteInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return satellites.NewDeleteSatelliteUnauthorized().WithPayload(errPayload)
	}

	ctx := params.HTTPRequest.Context()
	tx, err := svc.db.BeginTx(ctx, nil)
	if err != nil {
		return satellites.NewDeleteSatelliteInternalServerError().WithPayload(appError("Error: Failed to start database transaction", http.StatusInternalServerError))
	}
	q := svc.queries.WithTx(tx)
	committed := false
	defer rollbackUnlessCommitted(tx, &committed)

	sat, err := q.GetSatelliteByName(ctx, params.Satellite)
	if err != nil {
		return satellites.NewDeleteSatelliteBadRequest().WithPayload(appError("Error: Satellite Not Found", http.StatusBadRequest))
	}

	robotAcc, err := q.GetRobotAccBySatelliteID(ctx, sat.ID)
	if err != nil {
		return satellites.NewDeleteSatelliteInternalServerError().WithPayload(appError("Error: Failed to Delete Satellite", http.StatusInternalServerError))
	}

	robotID, err := strconv.ParseInt(robotAcc.RobotID, 10, 64)
	if err != nil {
		return satellites.NewDeleteSatelliteInternalServerError().WithPayload(appError("Error: Failed to Delete Satellite", http.StatusInternalServerError))
	}

	if err := q.DeleteSatelliteByName(ctx, params.Satellite); err != nil {
		return satellites.NewDeleteSatelliteInternalServerError().WithPayload(appError("Error: Failed to Delete Satellite", http.StatusInternalServerError))
	}

	if _, err := harbor.DeleteRobotAccount(ctx, robotID); err != nil {
		return satellites.NewDeleteSatelliteInternalServerError().WithPayload(appError("Error: Failed to Delete Satellite", http.StatusInternalServerError))
	}

	if err := utils.DeleteArtifact(utils.ConstructHarborDeleteURL(sat.Name, "satellite")); err != nil {
		return satellites.NewDeleteSatelliteInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	if err := tx.Commit(); err != nil {
		return satellites.NewDeleteSatelliteInternalServerError().WithPayload(appError("Error: Could not commit transaction", http.StatusInternalServerError))
	}
	committed = true

	return satellites.NewDeleteSatelliteOK().WithPayload(map[string]string{})
}

func GetCachedImages(params satellites.GetCachedImagesParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return satellites.NewGetCachedImagesInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return satellites.NewGetCachedImagesUnauthorized().WithPayload(errPayload)
	}

	satellite, err := svc.queries.GetSatelliteByName(params.HTTPRequest.Context(), params.Satellite)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return satellites.NewGetCachedImagesNotFound().WithPayload(appError("Satellite not found", http.StatusNotFound))
		}
		return satellites.NewGetCachedImagesInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	rows, err := svc.queries.GetLatestArtifacts(params.HTTPRequest.Context(), satellite.ID)
	if err != nil {
		return satellites.NewGetCachedImagesInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	response := make([]*swaggermodels.APIDatabaseArtifact, 0, len(rows))
	for _, row := range rows {
		response = append(response, apiArtifact(row))
	}

	return satellites.NewGetCachedImagesOK().WithPayload(response)
}

func GetSatellite(params satellites.GetSatelliteParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return satellites.NewGetSatelliteInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return satellites.NewGetSatelliteUnauthorized().WithPayload(errPayload)
	}

	satellite, err := svc.queries.GetSatelliteByName(params.HTTPRequest.Context(), params.Satellite)
	if err != nil {
		return satellites.NewGetSatelliteInternalServerError().WithPayload(appError("Satellite could not be loaded", http.StatusInternalServerError))
	}

	return satellites.NewGetSatelliteOK().WithPayload(apiSatellite(satellite))
}

func GetSatelliteStatus(params satellites.GetSatelliteStatusParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return satellites.NewGetSatelliteStatusInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return satellites.NewGetSatelliteStatusUnauthorized().WithPayload(errPayload)
	}

	satellite, err := svc.queries.GetSatelliteByName(params.HTTPRequest.Context(), params.Satellite)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return satellites.NewGetSatelliteStatusNotFound().WithPayload(appError("satellite not found", http.StatusNotFound))
		}
		return satellites.NewGetSatelliteStatusInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	status, err := svc.queries.GetLatestSatelliteStatus(params.HTTPRequest.Context(), satellite.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return satellites.NewGetSatelliteStatusNotFound().WithPayload(appError("no status available", http.StatusNotFound))
		}
		return satellites.NewGetSatelliteStatusInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	return satellites.NewGetSatelliteStatusOK().WithPayload(apiSatelliteStatus(status))
}

func ListActiveSatellites(params satellites.ListActiveSatellitesParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return satellites.NewListActiveSatellitesInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return satellites.NewListActiveSatellitesUnauthorized().WithPayload(errPayload)
	}

	rows, err := svc.queries.GetActiveSatellites(params.HTTPRequest.Context())
	if err != nil {
		return satellites.NewListActiveSatellitesInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	response := make([]*swaggermodels.APIActiveSatellite, 0, len(rows))
	for _, row := range rows {
		response = append(response, apiActiveSatellite(row))
	}

	return satellites.NewListActiveSatellitesOK().WithPayload(response)
}

func ListSatellites(params satellites.ListSatellitesParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return satellites.NewListSatellitesInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return satellites.NewListSatellitesUnauthorized().WithPayload(errPayload)
	}

	rows, err := svc.queries.ListSatellites(params.HTTPRequest.Context())
	if err != nil {
		return satellites.NewListSatellitesInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	response := make([]*swaggermodels.APISatellite, 0, len(rows))
	for _, row := range rows {
		response = append(response, apiSatellite(row))
	}

	return satellites.NewListSatellitesOK().WithPayload(response)
}

func ListStaleSatellites(params satellites.ListStaleSatellitesParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return satellites.NewListStaleSatellitesInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return satellites.NewListStaleSatellitesUnauthorized().WithPayload(errPayload)
	}

	rows, err := svc.queries.GetStaleSatellites(params.HTTPRequest.Context())
	if err != nil {
		return satellites.NewListStaleSatellitesInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	response := make([]*swaggermodels.APIStaleSatellite, 0, len(rows))
	for _, row := range rows {
		response = append(response, apiStaleSatellite(row))
	}

	return satellites.NewListStaleSatellitesOK().WithPayload(response)
}

func RegisterSatellite(params satellites.RegisterSatelliteParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return satellites.NewRegisterSatelliteUnauthorized().WithPayload(errPayload)
	}
	if params.Body == nil {
		return satellites.NewRegisterSatelliteBadRequest().WithPayload(appError("Invalid request body", http.StatusBadRequest))
	}
	req := params.Body
	if !utils.IsValidName(req.Name) {
		return satellites.NewRegisterSatelliteBadRequest().WithPayload(appError("Invalid satellite name: must be 1-255 chars, start with letter/number, and contain only lowercase letters, numbers, and ._-", http.StatusBadRequest))
	}
	if !utils.IsValidName(req.ConfigName) {
		return satellites.NewRegisterSatelliteBadRequest().WithPayload(appError("invalid or empty config_name", http.StatusBadRequest))
	}

	ctx := params.HTTPRequest.Context()
	roboPresent, err := harbor.IsRobotPresent(ctx, req.Name)
	if err != nil {
		return satellites.NewRegisterSatelliteBadRequest().WithPayload(appError(fmt.Sprintf("Error querying for robot account: %v", err), http.StatusBadRequest))
	}
	if roboPresent {
		return satellites.NewRegisterSatelliteBadRequest().WithPayload(appError("Error: Robot Account name already present. Try with different name", http.StatusBadRequest))
	}

	tx, err := svc.db.BeginTx(ctx, nil)
	if err != nil {
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	q := svc.queries.WithTx(tx)
	committed := false
	var robotID int64
	defer func() {
		if !committed && robotID != 0 {
			if _, delErr := harbor.DeleteRobotAccount(ctx, robotID); delErr != nil {
				log.Printf("Warning: Failed to cleanup robot account: %v", delErr)
			}
		}
		rollbackUnlessCommitted(tx, &committed)
	}()

	satellite, err := q.CreateSatellite(ctx, req.Name)
	if err != nil {
		return satellites.NewRegisterSatelliteBadRequest().WithPayload(appError("Error: failed to create satellite", http.StatusBadRequest))
	}

	groupStates, err := addSatelliteToGroups(ctx, q, req.Groups, satellite.ID)
	if err != nil {
		return satellites.NewRegisterSatelliteBadRequest().WithPayload(appError(err.Error(), http.StatusBadRequest))
	}

	if err := ensureSatelliteProjectExists(ctx); err != nil {
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	rbt, err := utils.CreateRobotAccForSatellite(ctx, []string{"satellite"}, satellite.Name)
	if err != nil {
		return satellites.NewRegisterSatelliteBadRequest().WithPayload(appError("Error: failed to create robot account", http.StatusBadRequest))
	}
	robotID = rbt.ID

	expiry := sql.NullTime{}
	if rbt.ExpiresAt > 0 {
		expiry = sql.NullTime{Time: time.Unix(rbt.ExpiresAt, 0), Valid: true}
	}
	if err := storeRobotAccount(ctx, q, rbt.Name, rbt.Secret, strconv.FormatInt(rbt.ID, 10), satellite.ID, expiry); err != nil {
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(appError("Error: failed to store robot account", http.StatusInternalServerError))
	}

	if err := assignPermissionsToRobot(ctx, q, req.Groups, rbt.ID); err != nil {
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(appError(err.Error(), http.StatusInternalServerError))
	}

	configObject, err := q.GetConfigByName(ctx, req.ConfigName)
	if err != nil {
		return satellites.NewRegisterSatelliteBadRequest().WithPayload(appError("Error: Config Not Found", http.StatusBadRequest))
	}
	if err := q.SetSatelliteConfig(ctx, database.SetSatelliteConfigParams{
		SatelliteID: satellite.ID,
		ConfigID:    configObject.ID,
	}); err != nil {
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	if err := utils.CreateOrUpdateSatStateArtifact(ctx, req.Name, groupStates, req.ConfigName); err != nil {
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	token, err := generateRandomToken(32)
	if err != nil {
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	tokenValue, err := q.AddToken(ctx, database.AddTokenParams{
		SatelliteID: satellite.ID,
		Token:       token,
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	})
	if err != nil {
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	if err := tx.Commit(); err != nil {
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(appError("Error: Could not commit transaction", http.StatusInternalServerError))
	}
	committed = true

	return satellites.NewRegisterSatelliteOK().WithPayload(&swaggermodels.RegisterSatelliteResponse{Token: tokenValue})
}

func SpiffeZtr(params satellites.SpiffeZtrParams) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return satellites.NewSpiffeZtrInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	ctx := params.HTTPRequest.Context()
	satelliteName, ok := spiffe.GetSatelliteName(ctx)
	if !ok {
		spiffeID, ok := spiffe.GetSPIFFEID(ctx)
		if !ok {
			return satellites.NewSpiffeZtrUnauthorized().WithPayload(appError("Error: SPIFFE authentication required", http.StatusUnauthorized))
		}
		var err error
		satelliteName, err = spiffe.ExtractSatelliteNameFromSPIFFEID(spiffeID)
		if err != nil {
			return satellites.NewSpiffeZtrBadRequest().WithPayload(appError("Error: Invalid SPIFFE ID format for satellite", http.StatusBadRequest))
		}
	}

	satellite, err := svc.queries.GetSatelliteByName(ctx, satelliteName)
	if err != nil {
		satellite, err = autoRegisterSatellite(ctx, svc, satelliteName)
		if err != nil {
			return satellites.NewSpiffeZtrInternalServerError().WithPayload(appError("Error: failed to auto-register satellite", http.StatusInternalServerError))
		}
	}

	var freshSecret string
	robot, err := svc.queries.GetRobotAccBySatelliteID(ctx, satellite.ID)
	if err != nil {
		var harborRobotID int64
		robot, harborRobotID, freshSecret, err = ensureSatelliteRobotAccount(ctx, svc.queries, satellite)
		if err != nil {
			return satellites.NewSpiffeZtrInternalServerError().WithPayload(appError("Error: failed to create robot account", http.StatusInternalServerError))
		}
		if err := ensureSatelliteConfig(ctx, svc.queries, satellite); err != nil {
			if harborRobotID != 0 {
				if _, delErr := harbor.DeleteRobotAccount(ctx, harborRobotID); delErr != nil {
					log.Printf("Warning: Failed to cleanup robot account %d after config failure: %v", harborRobotID, delErr)
				}
			}
			return satellites.NewSpiffeZtrInternalServerError().WithPayload(appError("Error: failed to ensure satellite config", http.StatusInternalServerError))
		}
	} else {
		freshSecret, err = refreshRobotSecret(ctx, svc.queries, robot)
		if err != nil {
			return satellites.NewSpiffeZtrInternalServerError().WithPayload(appError("Error: failed to refresh robot secret", http.StatusInternalServerError))
		}
	}

	groups, err := svc.queries.SatelliteGroupList(ctx, satellite.ID)
	if err != nil {
		return satellites.NewSpiffeZtrInternalServerError().WithPayload(appError("Error: Satellite Groups List Failed", http.StatusInternalServerError))
	}
	states, err := getGroupStates(ctx, groups, svc.queries)
	if err != nil {
		return satellites.NewSpiffeZtrInternalServerError().WithPayload(appError("Error: Get Group By ID Failed", http.StatusInternalServerError))
	}

	harborCfg := env.GC.Harbor
	var satelliteState string
	if !harborCfg.SkipHealthCheck {
		configObject, errPayload := fetchSatelliteConfig(ctx, svc.queries, satellite.ID)
		if errPayload != nil {
			return satellites.NewSpiffeZtrInternalServerError().WithPayload(errPayload)
		}
		if err := utils.CreateOrUpdateSatStateArtifact(ctx, satellite.Name, states, configObject.ConfigName); err != nil {
			return satellites.NewSpiffeZtrInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
		}
		satelliteState = utils.AssembleSatelliteState(satellite.Name)
	} else {
		satelliteState = "placeholder://spiffe-testing/" + satellite.Name
	}

	harborURL := harborCfg.URL
	if harborURL == "" {
		harborURL = "http://placeholder-registry:5000"
	}

	return satellites.NewSpiffeZtrOK().WithPayload(apiStateConfig(satelliteState, robot.RobotName, freshSecret, harborURL))
}

func SyncSatellite(params satellites.SyncSatelliteParams) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return satellites.NewSyncSatelliteInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if params.Body == nil {
		return satellites.NewSyncSatelliteBadRequest().WithPayload(appError("Invalid request body", http.StatusBadRequest))
	}

	req := params.Body
	satelliteName := req.Name
	if name, ok := spiffe.GetSatelliteName(params.HTTPRequest.Context()); ok {
		satelliteName = name
	}
	sat, err := svc.queries.GetSatelliteByName(params.HTTPRequest.Context(), satelliteName)
	if err != nil {
		return satellites.NewSyncSatelliteForbidden().WithPayload(appError("unknown satellite entity", http.StatusForbidden))
	}

	normalizedInterval, err := normalizeHeartbeatInterval(req.StateReportInterval)
	if err != nil {
		return satellites.NewSyncSatelliteBadRequest().WithPayload(appError("invalid heartbeat interval format", http.StatusBadRequest))
	}

	var artifactIDs []int32
	if len(req.CachedImages) > 0 {
		refs := make([]string, 0, len(req.CachedImages))
		sizes := make([]int64, 0, len(req.CachedImages))
		for _, img := range req.CachedImages {
			if img == nil {
				continue
			}
			refs = append(refs, img.Reference)
			sizes = append(sizes, img.SizeBytes)
		}

		if len(refs) > 0 {
			if err := svc.queries.BatchInsertArtifacts(params.HTTPRequest.Context(), database.BatchInsertArtifactsParams{
				Refs:  refs,
				Sizes: sizes,
			}); err != nil {
				return satellites.NewSyncSatelliteInternalServerError().WithPayload(appError("failed to save artifacts", http.StatusInternalServerError))
			}

			artifacts, err := svc.queries.GetArtifactIDsByReferences(params.HTTPRequest.Context(), refs)
			if err != nil {
				return satellites.NewSyncSatelliteInternalServerError().WithPayload(appError("failed to resolve artifact IDs", http.StatusInternalServerError))
			}

			artifactIDs = make([]int32, len(artifacts))
			for i, artifact := range artifacts {
				artifactIDs[i] = artifact.ID
			}
		}
	}

	reportedAt := time.Time(req.RequestCreatedTime)
	if reportedAt.IsZero() {
		reportedAt = time.Now()
	}

	if _, err := svc.queries.InsertSatelliteStatus(params.HTTPRequest.Context(), database.InsertSatelliteStatusParams{
		SatelliteID:        sat.ID,
		Activity:           req.Activity,
		LatestStateDigest:  toNullString(req.LatestStateDigest),
		LatestConfigDigest: toNullString(req.LatestConfigDigest),
		CpuPercent:         toNullString(fmt.Sprintf("%.2f", req.CPUPercent)),
		MemoryUsedBytes:    toNullInt64(int64(req.MemoryUsedBytes)),
		StorageUsedBytes:   toNullInt64(int64(req.StorageUsedBytes)),
		LastSyncDurationMs: toNullInt64(req.LastSyncDurationMs),
		ImageCount:         toNullInt32(int32(req.ImageCount)),
		ReportedAt:         reportedAt,
		ArtifactIds:        artifactIDs,
	}); err != nil {
		return satellites.NewSyncSatelliteInternalServerError().WithPayload(appError("failed to save status", http.StatusInternalServerError))
	}

	if err := svc.queries.UpdateSatelliteLastSeen(params.HTTPRequest.Context(), database.UpdateSatelliteLastSeenParams{
		ID:                sat.ID,
		HeartbeatInterval: toNullString(normalizedInterval),
	}); err != nil {
		return satellites.NewSyncSatelliteInternalServerError().WithPayload(appError("failed to update last_seen", http.StatusInternalServerError))
	}

	return satellites.NewSyncSatelliteOK()
}

func Ztr(params satellites.ZtrParams) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return satellites.NewZtrInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	ctx := params.HTTPRequest.Context()
	tokenInfo, err := svc.queries.GetTokenByValue(ctx, params.Token)
	if err != nil {
		return satellites.NewZtrBadRequest().WithPayload(appError("Error: Invalid Token", http.StatusBadRequest))
	}
	if time.Now().After(tokenInfo.ExpiresAt) {
		return satellites.NewZtrUnauthorized().WithPayload(appError("Error: Token Expired", http.StatusUnauthorized))
	}

	robot, err := svc.queries.GetRobotAccBySatelliteID(ctx, tokenInfo.SatelliteID)
	if err != nil {
		return satellites.NewZtrInternalServerError().WithPayload(appError("Error: Robot Account Not Found for Satellite", http.StatusInternalServerError))
	}
	freshSecret, err := refreshRobotSecret(ctx, svc.queries, robot)
	if err != nil {
		return satellites.NewZtrInternalServerError().WithPayload(appError("Error: failed to refresh robot secret", http.StatusInternalServerError))
	}

	groups, err := svc.queries.SatelliteGroupList(ctx, tokenInfo.SatelliteID)
	if err != nil {
		return satellites.NewZtrInternalServerError().WithPayload(appError("Error: Satellite Groups List Failed", http.StatusInternalServerError))
	}
	states, err := getGroupStates(ctx, groups, svc.queries)
	if err != nil {
		return satellites.NewZtrInternalServerError().WithPayload(appError("Error: Get Group By ID Failed", http.StatusInternalServerError))
	}

	satellite, err := svc.queries.GetSatellite(ctx, tokenInfo.SatelliteID)
	if err != nil {
		return satellites.NewZtrInternalServerError().WithPayload(appError("satellite not found", http.StatusNotFound))
	}

	configObject, errPayload := fetchSatelliteConfig(ctx, svc.queries, tokenInfo.SatelliteID)
	if errPayload != nil {
		return satellites.NewZtrInternalServerError().WithPayload(errPayload)
	}
	if err := utils.CreateOrUpdateSatStateArtifact(ctx, satellite.Name, states, configObject.ConfigName); err != nil {
		return satellites.NewZtrInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	if err := svc.queries.DeleteToken(ctx, params.Token); err != nil {
		return satellites.NewZtrInternalServerError().WithPayload(appError("Error: Error deleting token", http.StatusInternalServerError))
	}

	return satellites.NewZtrOK().WithPayload(apiStateConfig(utils.AssembleSatelliteState(satellite.Name), robot.RobotName, freshSecret, env.GC.Harbor.URL))
}

func toNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func toNullInt64(n int64) sql.NullInt64 {
	return sql.NullInt64{Int64: n, Valid: true}
}

func toNullInt32(n int32) sql.NullInt32 {
	return sql.NullInt32{Int32: n, Valid: true}
}

func normalizeHeartbeatInterval(interval string) (string, error) {
	if interval == "" {
		return "", nil
	}

	const prefix = "@every "
	if !strings.HasPrefix(interval, prefix) {
		return "", fmt.Errorf("invalid heartbeat interval format: must start with %q", prefix)
	}

	durationStr := strings.TrimPrefix(interval, prefix)
	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return "", fmt.Errorf("invalid heartbeat interval duration %q: %w", durationStr, err)
	}
	if duration <= 0 {
		return "", fmt.Errorf("heartbeat interval must be positive, got %v", duration)
	}
	if duration < time.Second {
		return "", fmt.Errorf("heartbeat interval must be at least 1 second, got %v", duration)
	}
	if duration.Nanoseconds()%int64(time.Second) != 0 {
		return "", fmt.Errorf("heartbeat interval must be a whole number of seconds, got %v", duration)
	}

	hours := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60
	seconds := int(duration.Seconds()) % 60

	return fmt.Sprintf("@every %02dh%02dm%02ds", hours, minutes, seconds), nil
}

func addSatelliteToGroups(ctx context.Context, q *database.Queries, groups []string, satelliteID int32) ([]string, error) {
	var groupStates []string
	for _, groupName := range groups {
		replications, err := harbor.ListReplication(ctx, harbor.ListParams{
			Q: fmt.Sprintf("name=%s", groupName),
		})
		if len(replications) < 1 {
			if err != nil {
				return nil, fmt.Errorf("group name %s does not exist in replication; please give a valid group name", groupName)
			}
		}

		group, err := q.GetGroupByName(ctx, groupName)
		if err != nil {
			return nil, fmt.Errorf("invalid group name: %s", groupName)
		}
		if err := q.AddSatelliteToGroup(ctx, database.AddSatelliteToGroupParams{
			SatelliteID: satelliteID,
			GroupID:     group.ID,
		}); err != nil {
			return nil, err
		}

		groupStates = append(groupStates, utils.AssembleGroupState(groupName))
	}
	return groupStates, nil
}

func storeRobotAccount(ctx context.Context, q *database.Queries, robotName, secret, robotID string, satelliteID int32, expiry sql.NullTime) error {
	secretHash, err := crypto.HashSecret(secret)
	if err != nil {
		return err
	}
	_, err = q.AddRobotAccount(ctx, database.AddRobotAccountParams{
		RobotName:       robotName,
		RobotSecretHash: secretHash,
		RobotID:         robotID,
		SatelliteID:     satelliteID,
		RobotExpiry:     expiry,
	})
	return err
}

func assignPermissionsToRobot(ctx context.Context, q *database.Queries, groups []string, robotID int64) error {
	for _, groupName := range groups {
		projects, err := q.GetProjectsOfGroup(ctx, groupName)
		if err != nil {
			return fmt.Errorf("failed to fetch projects for group")
		}
		if len(projects) == 0 {
			return fmt.Errorf("no projects found for group")
		}
		if _, err := utils.UpdateRobotProjects(ctx, projects[0], strconv.FormatInt(robotID, 10)); err != nil {
			return fmt.Errorf("failed to update robot account permissions")
		}
	}
	return nil
}

func getGroupStates(ctx context.Context, groups []database.SatelliteGroup, q *database.Queries) ([]string, error) {
	states := make([]string, 0, len(groups))
	for _, group := range groups {
		grp, err := q.GetGroupByID(ctx, group.GroupID)
		if err != nil {
			return nil, err
		}
		states = append(states, utils.AssembleGroupState(grp.GroupName))
	}
	return states, nil
}

func generateRandomToken(charLength int) (string, error) {
	byteLength := charLength / 2
	token := make([]byte, byteLength)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	return hex.EncodeToString(token), nil
}

func refreshRobotSecret(ctx context.Context, q *database.Queries, robot database.RobotAccount) (string, error) {
	if env.GC.Harbor.SkipHealthCheck {
		return "spiffe-auto-registered-placeholder-secret", nil
	}

	harborRobotID, err := strconv.ParseInt(robot.RobotID, 10, 64)
	if err != nil {
		return "", fmt.Errorf("parse robot ID: %w", err)
	}

	resp, err := harbor.RefreshRobotAccount(ctx, "", harborRobotID)
	if err != nil {
		return "", fmt.Errorf("refresh robot secret in Harbor: %w", err)
	}
	if resp.Payload == nil || resp.Payload.Secret == "" {
		return "", fmt.Errorf("harbor returned empty secret for robot %s", robot.RobotName)
	}

	newHash, err := crypto.HashSecret(resp.Payload.Secret)
	if err != nil {
		return "", fmt.Errorf("hash refreshed secret: %w", err)
	}

	robotDetails, err := harbor.GetRobotAccount(ctx, harborRobotID)
	if err != nil {
		return "", fmt.Errorf("fetch updated robot details: %w", err)
	}

	var newExpiry sql.NullTime
	if robotDetails.ExpiresAt > 0 {
		newExpiry = sql.NullTime{Time: time.Unix(robotDetails.ExpiresAt, 0), Valid: true}
	}

	if err := q.UpdateRobotAccount(ctx, database.UpdateRobotAccountParams{
		ID:              robot.ID,
		RobotName:       robot.RobotName,
		RobotSecretHash: newHash,
		RobotID:         robot.RobotID,
		RobotExpiry:     newExpiry,
	}); err != nil {
		return "", fmt.Errorf("update robot hash in DB: %w", err)
	}

	return resp.Payload.Secret, nil
}

func ensureSatelliteRobotAccount(ctx context.Context, q *database.Queries, satellite database.Satellite) (database.RobotAccount, int64, string, error) {
	var robotName, robotSecret string
	var harborRobotID int64
	var expiry sql.NullTime

	if !env.GC.Harbor.SkipHealthCheck {
		if err := ensureSatelliteProjectExists(ctx); err != nil {
			return database.RobotAccount{}, 0, "", fmt.Errorf("ensure satellite project: %w", err)
		}

		rbt, err := utils.CreateRobotAccForSatellite(ctx, []string{"satellite"}, satellite.Name)
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
		robotName = "robot$satellite-" + satellite.Name
		robotSecret = "spiffe-auto-registered-placeholder-secret"
	}

	secretHash, err := crypto.HashSecret(robotSecret)
	if err != nil {
		return database.RobotAccount{}, harborRobotID, "", fmt.Errorf("hash robot credentials: %w", err)
	}
	robot, err := q.AddRobotAccount(ctx, database.AddRobotAccountParams{
		RobotName:       robotName,
		RobotSecretHash: secretHash,
		RobotID:         strconv.FormatInt(harborRobotID, 10),
		SatelliteID:     satellite.ID,
		RobotExpiry:     expiry,
	})
	if err != nil {
		return database.RobotAccount{}, harborRobotID, "", fmt.Errorf("store robot account: %w", err)
	}

	return robot, harborRobotID, robotSecret, nil
}

func ensureSatelliteConfig(ctx context.Context, q *database.Queries, satellite database.Satellite) error {
	if _, errPayload := fetchSatelliteConfig(ctx, q, satellite.ID); errPayload == nil {
		return nil
	}

	defaultConfig, err := q.GetConfigByName(ctx, "default")
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
		defaultConfig, err = q.CreateConfig(ctx, database.CreateConfigParams{
			ConfigName:  "default",
			RegistryUrl: env.GC.Harbor.URL,
			Config:      defaultConfigJSON,
		})
		if err != nil {
			return fmt.Errorf("create default config: %w", err)
		}

		if pushErr := utils.CreateAndPushConfigStateArtifact(ctx, defaultConfigJSON, "default"); pushErr != nil {
			log.Printf("SPIFFE ZTR: Warning - failed to create config-state artifact: %v", pushErr)
		}
	}

	if err := q.SetSatelliteConfig(ctx, database.SetSatelliteConfigParams{
		SatelliteID: satellite.ID,
		ConfigID:    defaultConfig.ID,
	}); err != nil {
		return fmt.Errorf("link satellite to config: %w", err)
	}

	return nil
}

func autoRegisterSatellite(ctx context.Context, svc *service, name string) (database.Satellite, error) {
	tx, err := svc.db.BeginTx(ctx, nil)
	if err != nil {
		return database.Satellite{}, fmt.Errorf("begin transaction: %w", err)
	}

	q := svc.queries.WithTx(tx)
	committed := false
	var harborRobotID int64
	defer func() {
		if !committed && harborRobotID != 0 {
			if _, delErr := harbor.DeleteRobotAccount(ctx, harborRobotID); delErr != nil {
				log.Printf("Warning: Failed to cleanup robot account during auto-register: %v", delErr)
			}
		}
		rollbackUnlessCommitted(tx, &committed)
	}()

	satellite, err := q.CreateSatellite(ctx, name)
	if err != nil {
		return database.Satellite{}, fmt.Errorf("create satellite: %w", err)
	}

	_, harborRobotID, _, err = ensureSatelliteRobotAccount(ctx, q, satellite)
	if err != nil {
		return database.Satellite{}, err
	}

	if err := ensureSatelliteConfig(ctx, q, satellite); err != nil {
		return database.Satellite{}, err
	}

	if err := tx.Commit(); err != nil {
		return database.Satellite{}, fmt.Errorf("commit transaction: %w", err)
	}
	committed = true

	return satellite, nil
}

func apiStateConfig(state, username, password, registryURL string) *swaggermodels.APIStateConfig {
	return &swaggermodels.APIStateConfig{
		State: state,
		Auth: &swaggermodels.APIRegistryCredentials{
			Username: username,
			Password: strfmt.Password(password),
			URL:      registryURL,
		},
	}
}
