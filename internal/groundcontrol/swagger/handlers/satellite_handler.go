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
	auditlog "github.com/container-registry/harbor-satellite/internal/groundcontrol/logger"
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
		return satellites.NewDeleteSatelliteInternalServerError().WithPayload(internalError("Failed to initialize satellite service", err))
	}
	actor, errPayload := requirePrincipal(principal)
	if errPayload != nil {
		return satellites.NewDeleteSatelliteUnauthorized().WithPayload(errPayload)
	}

	ctx := params.HTTPRequest.Context()
	tx, err := svc.db.BeginTx(ctx, nil)
	if err != nil {
		return satellites.NewDeleteSatelliteInternalServerError().WithPayload(internalError("Failed to start satellite deletion transaction", err))
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
		return satellites.NewDeleteSatelliteInternalServerError().WithPayload(internalError("Failed to load satellite robot account", err))
	}

	robotID, err := strconv.ParseInt(robotAcc.RobotID, 10, 64)
	if err != nil {
		return satellites.NewDeleteSatelliteInternalServerError().WithPayload(internalError("Satellite robot account has an invalid Harbor robot ID", err))
	}

	if err := q.DeleteSatelliteByName(ctx, params.Satellite); err != nil {
		return satellites.NewDeleteSatelliteInternalServerError().WithPayload(internalError("Failed to delete satellite record", err))
	}

	if err := tx.Commit(); err != nil {
		return satellites.NewDeleteSatelliteInternalServerError().WithPayload(internalError("Failed to commit satellite deletion transaction", err))
	}
	committed = true
	auditEvent := auditlog.AuditEvent{
		Operation:    auditlog.OpDeregister,
		ResourceType: auditlog.ResSatellite,
		Actor:        actor.Username,
		ActorType:    auditlog.ActorUser,
		SatelliteID:  sat.Name,
		Resource:     sat.Name,
	}

	var cleanupErrors []error
	if _, err := harbor.DeleteRobotAccount(ctx, robotID); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("delete Harbor robot account: %w", err))
	}

	if err := utils.DeleteArtifact(utils.ConstructHarborDeleteURL(sat.Name, "satellite")); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("delete satellite artifact: %w", err))
	}
	if cleanupErr := errors.Join(cleanupErrors...); cleanupErr != nil {
		auditEvent.Outcome = auditlog.OutcomeFailure
		auditEvent.Details = map[string]any{
			"database_deleted": true,
			"harbor_cleanup":   "incomplete",
			"cleanup_error":    cleanupErr.Error(),
		}
		svc.auditEvent(params.HTTPRequest, auditEvent)
		return satellites.NewDeleteSatelliteInternalServerError().WithPayload(internalError("Satellite was deleted, but Harbor cleanup was incomplete", cleanupErr))
	}
	auditEvent.Outcome = auditlog.OutcomeSuccess
	auditEvent.Details = map[string]any{
		"database_deleted": true,
		"harbor_cleanup":   "completed",
	}
	svc.auditEvent(params.HTTPRequest, auditEvent)
	return satellites.NewDeleteSatelliteOK().WithPayload(map[string]string{})
}

func GetCachedImages(params satellites.GetCachedImagesParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return satellites.NewGetCachedImagesInternalServerError().WithPayload(internalError("Failed to initialize satellite service", err))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return satellites.NewGetCachedImagesUnauthorized().WithPayload(errPayload)
	}

	satellite, err := svc.queries.GetSatelliteByName(params.HTTPRequest.Context(), params.Satellite)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return satellites.NewGetCachedImagesNotFound().WithPayload(appError("Satellite not found", http.StatusNotFound))
		}
		return satellites.NewGetCachedImagesInternalServerError().WithPayload(internalError("Failed to load satellite before listing cached images", err))
	}

	rows, err := svc.queries.GetLatestArtifacts(params.HTTPRequest.Context(), satellite.ID)
	if err != nil {
		return satellites.NewGetCachedImagesInternalServerError().WithPayload(internalError("Failed to load latest cached images", err))
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
		return satellites.NewGetSatelliteInternalServerError().WithPayload(internalError("Failed to initialize satellite service", err))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return satellites.NewGetSatelliteUnauthorized().WithPayload(errPayload)
	}

	satellite, err := svc.queries.GetSatelliteByName(params.HTTPRequest.Context(), params.Satellite)
	if err != nil {
		return satellites.NewGetSatelliteInternalServerError().WithPayload(internalError("Failed to load satellite", err))
	}

	return satellites.NewGetSatelliteOK().WithPayload(apiSatellite(satellite))
}

func GetSatelliteStatus(params satellites.GetSatelliteStatusParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return satellites.NewGetSatelliteStatusInternalServerError().WithPayload(internalError("Failed to initialize satellite service", err))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return satellites.NewGetSatelliteStatusUnauthorized().WithPayload(errPayload)
	}

	satellite, err := svc.queries.GetSatelliteByName(params.HTTPRequest.Context(), params.Satellite)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return satellites.NewGetSatelliteStatusNotFound().WithPayload(appError("satellite not found", http.StatusNotFound))
		}
		return satellites.NewGetSatelliteStatusInternalServerError().WithPayload(internalError("Failed to load satellite before retrieving status", err))
	}

	status, err := svc.queries.GetLatestSatelliteStatus(params.HTTPRequest.Context(), satellite.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return satellites.NewGetSatelliteStatusNotFound().WithPayload(appError("no status available", http.StatusNotFound))
		}
		return satellites.NewGetSatelliteStatusInternalServerError().WithPayload(internalError("Failed to load latest satellite status", err))
	}

	return satellites.NewGetSatelliteStatusOK().WithPayload(apiSatelliteStatus(status))
}

func ListActiveSatellites(params satellites.ListActiveSatellitesParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return satellites.NewListActiveSatellitesInternalServerError().WithPayload(internalError("Failed to initialize satellite service", err))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return satellites.NewListActiveSatellitesUnauthorized().WithPayload(errPayload)
	}

	rows, err := svc.queries.GetActiveSatellites(params.HTTPRequest.Context())
	if err != nil {
		return satellites.NewListActiveSatellitesInternalServerError().WithPayload(internalError("Failed to list active satellites", err))
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
		return satellites.NewListSatellitesInternalServerError().WithPayload(internalError("Failed to initialize satellite service", err))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return satellites.NewListSatellitesUnauthorized().WithPayload(errPayload)
	}

	rows, err := svc.queries.ListSatellites(params.HTTPRequest.Context())
	if err != nil {
		return satellites.NewListSatellitesInternalServerError().WithPayload(internalError("Failed to list satellites", err))
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
		return satellites.NewListStaleSatellitesInternalServerError().WithPayload(internalError("Failed to initialize satellite service", err))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return satellites.NewListStaleSatellitesUnauthorized().WithPayload(errPayload)
	}

	rows, err := svc.queries.GetStaleSatellites(params.HTTPRequest.Context())
	if err != nil {
		return satellites.NewListStaleSatellitesInternalServerError().WithPayload(internalError("Failed to list stale satellites", err))
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
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(internalError("Failed to initialize satellite service", err))
	}
	actor, errPayload := requirePrincipal(principal)
	if errPayload != nil {
		return satellites.NewRegisterSatelliteUnauthorized().WithPayload(errPayload)
	}
	if spiffeRegistrationEnabled() {
		return satellites.NewRegisterSatelliteBadRequest().WithPayload(appError(
			"satellite registration via this endpoint is disabled when SPIFFE is enabled. Use POST /api/satellites/register instead",
			http.StatusBadRequest,
		))
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
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(internalError("Failed to start satellite registration transaction", err))
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
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(internalError("Failed to ensure the Harbor satellite project exists", err))
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
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(internalError("Failed to store satellite robot account", err))
	}

	if err := assignPermissionsToRobot(ctx, q, req.Groups, rbt.ID); err != nil {
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(internalError("Failed to assign group permissions to satellite robot account", err))
	}

	configObject, err := q.GetConfigByName(ctx, req.ConfigName)
	if err != nil {
		return satellites.NewRegisterSatelliteBadRequest().WithPayload(appError("Error: Config Not Found", http.StatusBadRequest))
	}
	if err := q.SetSatelliteConfig(ctx, database.SetSatelliteConfigParams{
		SatelliteID: satellite.ID,
		ConfigID:    configObject.ID,
	}); err != nil {
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(internalError("Failed to assign configuration to satellite", err))
	}

	if err := utils.CreateOrUpdateSatStateArtifact(ctx, req.Name, groupStates, req.ConfigName); err != nil {
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(internalError("Failed to create or push satellite state artifact", err))
	}

	token, err := generateRandomToken(32)
	if err != nil {
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(internalError("Failed to generate satellite registration token", err))
	}
	tokenValue, err := q.AddToken(ctx, database.AddTokenParams{
		SatelliteID: satellite.ID,
		Token:       token,
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	})
	if err != nil {
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(internalError("Failed to store satellite registration token", err))
	}

	if err := tx.Commit(); err != nil {
		return satellites.NewRegisterSatelliteInternalServerError().WithPayload(internalError("Failed to commit satellite registration transaction", err))
	}
	committed = true
	svc.auditEvent(params.HTTPRequest, auditlog.AuditEvent{
		Operation:    auditlog.OpRegister,
		ResourceType: auditlog.ResSatellite,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        actor.Username,
		ActorType:    auditlog.ActorUser,
		SatelliteID:  satellite.Name,
		Resource:     satellite.Name,
		Details: map[string]any{
			"config_name": req.ConfigName,
			"groups":      req.Groups,
			"flow":        "token",
		},
	})

	return satellites.NewRegisterSatelliteOK().WithPayload(&swaggermodels.RegisterSatelliteResponse{Token: tokenValue})
}

func SpiffeZtr(params satellites.SpiffeZtrParams) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return satellites.NewSpiffeZtrInternalServerError().WithPayload(internalError("Failed to initialize satellite service", err))
	}

	ctx := params.HTTPRequest.Context()
	satelliteName, ok := spiffe.GetSatelliteName(ctx)
	if !ok {
		spiffeID, ok := spiffe.GetSPIFFEID(ctx)
		if !ok {
			svc.auditEvent(params.HTTPRequest, auditlog.AuditEvent{
				Operation:    auditlog.OpAuth,
				ResourceType: auditlog.ResSatellite,
				Outcome:      auditlog.OutcomeFailure,
				ActorType:    auditlog.ActorAnonymous,
				Reason:       auditlog.ReasonMissingSpiffeIdentity,
				Details:      map[string]any{"flow": "spiffe_ztr"},
			})
			return satellites.NewSpiffeZtrUnauthorized().WithPayload(appError("Error: SPIFFE authentication required", http.StatusUnauthorized))
		}
		var err error
		satelliteName, err = spiffe.ExtractSatelliteNameFromSPIFFEID(spiffeID)
		if err != nil {
			svc.auditEvent(params.HTTPRequest, auditlog.AuditEvent{
				Operation:    auditlog.OpAuth,
				ResourceType: auditlog.ResSatellite,
				Outcome:      auditlog.OutcomeFailure,
				Actor:        spiffeID.String(),
				ActorType:    auditlog.ActorAnonymous,
				Reason:       auditlog.ReasonInvalidSpiffeID,
				Details:      map[string]any{"flow": "spiffe_ztr"},
			})
			return satellites.NewSpiffeZtrBadRequest().WithPayload(appError("Error: Invalid SPIFFE ID format for satellite", http.StatusBadRequest))
		}
	}

	satellite, err := svc.queries.GetSatelliteByName(ctx, satelliteName)
	if err != nil {
		satellite, err = autoRegisterSatellite(ctx, svc, satelliteName)
		if err != nil {
			return satellites.NewSpiffeZtrInternalServerError().WithPayload(internalError("Failed to auto-register SPIFFE satellite", err))
		}
	}

	var freshSecret string
	robot, err := svc.queries.GetRobotAccBySatelliteID(ctx, satellite.ID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		robot, freshSecret, err = createSatelliteRobotAndConfig(ctx, svc, satellite)
		if err != nil {
			return satellites.NewSpiffeZtrInternalServerError().WithPayload(internalError("Failed to create SPIFFE satellite robot account", err))
		}
	case err != nil:
		return satellites.NewSpiffeZtrInternalServerError().WithPayload(internalError("Failed to load SPIFFE satellite robot account", err))
	default:
		freshSecret, err = refreshRobotSecret(ctx, svc.queries, robot)
		if err != nil {
			return satellites.NewSpiffeZtrInternalServerError().WithPayload(internalError("Failed to refresh SPIFFE satellite robot secret", err))
		}
	}

	groups, err := svc.queries.SatelliteGroupList(ctx, satellite.ID)
	if err != nil {
		return satellites.NewSpiffeZtrInternalServerError().WithPayload(internalError("Failed to list SPIFFE satellite groups", err))
	}
	states, err := getGroupStates(ctx, groups, svc.queries)
	if err != nil {
		return satellites.NewSpiffeZtrInternalServerError().WithPayload(internalError("Failed to load SPIFFE satellite group states", err))
	}

	harborCfg := env.GC.Harbor
	var satelliteState string
	if !harborCfg.SkipHealthCheck {
		configObject, errPayload := fetchSatelliteConfig(ctx, svc.queries, satellite.ID)
		if errPayload != nil {
			return satellites.NewSpiffeZtrInternalServerError().WithPayload(errPayload)
		}
		if err := utils.CreateOrUpdateSatStateArtifact(ctx, satellite.Name, states, configObject.ConfigName); err != nil {
			return satellites.NewSpiffeZtrInternalServerError().WithPayload(internalError("Failed to create or push SPIFFE satellite state artifact", err))
		}
		satelliteState = utils.AssembleSatelliteState(satellite.Name)
	} else {
		satelliteState = "placeholder://spiffe-testing/" + satellite.Name
	}

	harborURL := harborCfg.URL
	if harborURL == "" {
		harborURL = "http://placeholder-registry:5000"
	}
	svc.auditEvent(params.HTTPRequest, auditlog.AuditEvent{
		Operation:    auditlog.OpRegister,
		ResourceType: auditlog.ResSatellite,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        satellite.Name,
		ActorType:    auditlog.ActorSatellite,
		SatelliteID:  satellite.Name,
		Resource:     satellite.Name,
		Details:      map[string]any{"flow": "spiffe_ztr"},
	})

	return satellites.NewSpiffeZtrOK().WithPayload(apiStateConfig(satelliteState, robot.RobotName, freshSecret, harborURL))
}

func SyncSatellite(params satellites.SyncSatelliteParams) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return satellites.NewSyncSatelliteInternalServerError().WithPayload(internalError("Failed to initialize satellite service", err))
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
				return satellites.NewSyncSatelliteInternalServerError().WithPayload(internalError("Failed to save cached image artifacts", err))
			}

			artifacts, err := svc.queries.GetArtifactIDsByReferences(params.HTTPRequest.Context(), refs)
			if err != nil {
				return satellites.NewSyncSatelliteInternalServerError().WithPayload(internalError("Failed to resolve cached image artifact IDs", err))
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
		return satellites.NewSyncSatelliteInternalServerError().WithPayload(internalError("Failed to save satellite status report", err))
	}

	if err := svc.queries.UpdateSatelliteLastSeen(params.HTTPRequest.Context(), database.UpdateSatelliteLastSeenParams{
		ID:                sat.ID,
		HeartbeatInterval: toNullString(normalizedInterval),
	}); err != nil {
		return satellites.NewSyncSatelliteInternalServerError().WithPayload(internalError("Satellite status was saved, but last-seen time could not be updated", err))
	}

	return satellites.NewSyncSatelliteOK()
}

func Ztr(params satellites.ZtrParams) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return satellites.NewZtrInternalServerError().WithPayload(internalError("Failed to initialize satellite service", err))
	}

	ctx := params.HTTPRequest.Context()
	tokenInfo, err := svc.queries.GetTokenByValue(ctx, params.Token)
	if err != nil {
		svc.auditEvent(params.HTTPRequest, auditlog.AuditEvent{
			Operation:    auditlog.OpAuth,
			ResourceType: auditlog.ResSatellite,
			Outcome:      auditlog.OutcomeFailure,
			ActorType:    auditlog.ActorAnonymous,
			Reason:       auditlog.ReasonInvalidToken,
			Details:      map[string]any{"masked_token": maskToken(params.Token)},
		})
		return satellites.NewZtrBadRequest().WithPayload(appError("Error: Invalid Token", http.StatusBadRequest))
	}
	if time.Now().After(tokenInfo.ExpiresAt) {
		svc.auditEvent(params.HTTPRequest, auditlog.AuditEvent{
			Operation:    auditlog.OpAuth,
			ResourceType: auditlog.ResSatellite,
			Outcome:      auditlog.OutcomeFailure,
			ActorType:    auditlog.ActorAnonymous,
			Reason:       auditlog.ReasonTokenExpired,
			Details: map[string]any{
				"masked_token": maskToken(params.Token),
				"expired_at":   tokenInfo.ExpiresAt.Format(time.RFC3339),
			},
		})
		return satellites.NewZtrUnauthorized().WithPayload(appError("Error: Token Expired", http.StatusUnauthorized))
	}

	robot, err := svc.queries.GetRobotAccBySatelliteID(ctx, tokenInfo.SatelliteID)
	if err != nil {
		return satellites.NewZtrInternalServerError().WithPayload(internalError("Failed to load satellite robot account", err))
	}
	freshSecret, err := refreshRobotSecret(ctx, svc.queries, robot)
	if err != nil {
		return satellites.NewZtrInternalServerError().WithPayload(internalError("Failed to refresh satellite robot secret", err))
	}

	groups, err := svc.queries.SatelliteGroupList(ctx, tokenInfo.SatelliteID)
	if err != nil {
		return satellites.NewZtrInternalServerError().WithPayload(internalError("Failed to list satellite groups", err))
	}
	states, err := getGroupStates(ctx, groups, svc.queries)
	if err != nil {
		return satellites.NewZtrInternalServerError().WithPayload(internalError("Failed to load satellite group states", err))
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
		return satellites.NewZtrInternalServerError().WithPayload(internalError("Failed to create or push satellite state artifact", err))
	}

	if err := svc.queries.DeleteToken(ctx, params.Token); err != nil {
		return satellites.NewZtrInternalServerError().WithPayload(internalError("Registration succeeded, but the single-use token could not be deleted", err))
	}
	svc.auditEvent(params.HTTPRequest, auditlog.AuditEvent{
		Operation:    auditlog.OpRegister,
		ResourceType: auditlog.ResSatellite,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        satellite.Name,
		ActorType:    auditlog.ActorSatellite,
		SatelliteID:  satellite.Name,
		Resource:     satellite.Name,
		Details:      map[string]any{"flow": "ztr"},
	})

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
		if err != nil {
			return nil, fmt.Errorf("list replication policies for group %s: %w", groupName, err)
		}
		if len(replications) < 1 {
			return nil, fmt.Errorf("group name %s does not exist in replication; please give a valid group name", groupName)
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

func maskToken(token string) string {
	if len(token) < 8 {
		return "***"
	}
	return fmt.Sprintf("%s...%s", token[:4], token[len(token)-4:])
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

func createSatelliteRobotAndConfig(ctx context.Context, svc *service, satellite database.Satellite) (database.RobotAccount, string, error) {
	tx, err := svc.db.BeginTx(ctx, nil)
	if err != nil {
		return database.RobotAccount{}, "", fmt.Errorf("begin robot and config transaction: %w", err)
	}

	q := svc.queries.WithTx(tx)
	committed := false
	var harborRobotID int64
	defer func() {
		if !committed && harborRobotID != 0 {
			if _, deleteErr := harbor.DeleteRobotAccount(ctx, harborRobotID); deleteErr != nil {
				log.Printf("Warning: Failed to cleanup robot account %d: %v", harborRobotID, deleteErr)
			}
		}
		rollbackUnlessCommitted(tx, &committed)
	}()

	robot, harborRobotID, secret, err := ensureSatelliteRobotAccount(ctx, q, satellite)
	if err != nil {
		return database.RobotAccount{}, "", err
	}
	if err := ensureSatelliteConfig(ctx, q, satellite); err != nil {
		return database.RobotAccount{}, "", fmt.Errorf("ensure satellite config: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return database.RobotAccount{}, "", fmt.Errorf("commit robot and config transaction: %w", err)
	}
	committed = true
	return robot, secret, nil
}

func spiffeRegistrationEnabled() bool {
	return env.GC.SPIFFE.Enabled || env.GC.EmbeddedSPIRE.Enabled || env.GC.SPIRE.ServerSocket != ""
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
