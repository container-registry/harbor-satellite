package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	swaggermodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/models"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/satellites"
	"github.com/go-openapi/runtime/middleware"
)

func DeleteSatellite(params satellites.DeleteSatelliteParams, principal any) middleware.Responder {
	_ = params
	_ = principal
	return notImplemented("operation satellites.DeleteSatellite has not yet been implemented")
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
	_ = params
	_ = principal
	return notImplemented("operation satellites.RegisterSatellite has not yet been implemented")
}

func SpiffeZtr(params satellites.SpiffeZtrParams) middleware.Responder {
	_ = params
	return notImplemented("operation satellites.SpiffeZtr has not yet been implemented")
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
	_ = params
	return notImplemented("operation satellites.Ztr has not yet been implemented")
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
