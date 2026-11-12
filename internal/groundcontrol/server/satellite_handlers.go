package server

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
)

func (s *Server) SyncSatellite(w http.ResponseWriter, r *http.Request) {
	if s.satelliteAuth != nil {
		s.syncWithAuth(w, r)
		return
	}
	resp := SatelliteSyncResponse{
		Events: make([]string, 0),
	}
	var req SatelliteStatusRequest
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

	sat, err := s.fleet().GetSatelliteByName(r.Context(), satelliteName)
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

	robotAcc, err := s.fleet().GetRobotAccBySatelliteID(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Failed to find robot account for satellite : %s", satelliteName)
		HandleAppError(w, &AppError{
			Message: "Failed to find robot account",
			Code:    http.StatusForbidden,
		})
		return
	}

	if robotAcc.RobotExpiry.Valid {
		duration, err := time.ParseDuration(strings.TrimPrefix(normalizedInterval, "@every "))
		if err != nil {
			log.Printf("Invalid heartbeat interval %q: %v", req.StateReportInterval, err)
			HandleAppError(w, &AppError{Message: "invalid heartbeat interval format", Code: http.StatusBadRequest})
			return
		}

		// Basically checks whether the robot expires before the 2nd state sync from now
		// or not
		future := time.Now().Add(duration).Add(duration)
		if robotAcc.RobotExpiry.Time.Before(future) {
			resp.Events = append(resp.Events, "refresh_credentials")
		}
	}

	var artifactIDs []int32
	if len(req.CachedImages) > 0 {
		refs := make([]string, len(req.CachedImages))
		sizes := make([]int64, len(req.CachedImages))
		for i, img := range req.CachedImages {
			refs[i] = img.Reference
			sizes[i] = img.SizeBytes
		}

		err := s.fleet().BatchInsertArtifacts(r.Context(), database.BatchInsertArtifactsParams{
			Refs:  refs,
			Sizes: sizes,
		})
		if err != nil {
			log.Printf("Failed to batch insert artifacts: %v", err)
			HandleAppError(w, &AppError{Message: "failed to save artifacts", Code: http.StatusInternalServerError})
			return
		}

		artifacts, err := s.fleet().GetArtifactIDsByReferences(r.Context(), refs)
		if err != nil {
			log.Printf("Failed to resolve artifact IDs: %v", err)
			HandleAppError(w, &AppError{Message: "failed to resolve artifact IDs", Code: http.StatusInternalServerError})
			return
		}

		artifactIDs = make([]int32, len(artifacts))
		for i, a := range artifacts {
			artifactIDs[i] = a.ID
		}
		log.Printf("Stored %d artifacts for satellite %s", len(artifactIDs), satelliteName)
	}

	_, err = s.fleet().InsertSatelliteStatus(r.Context(), database.InsertSatelliteStatusParams{
		SatelliteID:        sat.ID,
		Activity:           req.Activity,
		LatestStateDigest:  toNullString(req.LatestStateDigest),
		LatestConfigDigest: toNullString(req.LatestConfigDigest),
		CpuPercent:         toNullString(fmt.Sprintf("%.2f", req.CPUPercent)),
		MemoryUsedBytes:    toNullInt64(req.MemoryUsedBytes),
		StorageUsedBytes:   toNullInt64(req.StorageUsedBytes),
		LastSyncDurationMs: toNullInt64(req.LastSyncDurationMs),
		ImageCount:         toNullInt32(req.ImageCount),
		ReportedAt:         req.RequestCreatedTime,
		ArtifactIds:        artifactIDs,
	})
	if err != nil {
		log.Printf("Failed to insert status: %v", err)
		HandleAppError(w, &AppError{Message: "failed to save status", Code: http.StatusInternalServerError})
		return
	}

	err = s.fleet().UpdateSatelliteLastSeen(r.Context(), database.UpdateSatelliteLastSeenParams{
		ID:                sat.ID,
		HeartbeatInterval: toNullString(normalizedInterval),
	})
	if err != nil {
		log.Printf("Failed to update last_seen: %v", err)
		HandleAppError(w, &AppError{Message: "failed to update last_seen", Code: http.StatusInternalServerError})
		return
	}

	WriteJSONResponse(w, http.StatusOK, resp)
}

func (s *Server) GetSatelliteStatus(w http.ResponseWriter, r *http.Request, satelliteName string) {
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

func (s *Server) ListActiveSatellites(w http.ResponseWriter, r *http.Request) {
	satellites, err := s.dbQueries.GetActiveSatellites(r.Context())
	if err != nil {
		log.Printf("Failed to get active satellites: %v", err)
		HandleAppError(w, &AppError{Message: "failed to get active satellites", Code: http.StatusInternalServerError})
		return
	}
	WriteJSONResponse(w, http.StatusOK, satellites)
}

func (s *Server) ListStaleSatellites(w http.ResponseWriter, r *http.Request) {
	satellites, err := s.dbQueries.GetStaleSatellites(r.Context())
	if err != nil {
		log.Printf("Failed to get stale satellites: %v", err)
		HandleAppError(w, &AppError{Message: "failed to get stale satellites", Code: http.StatusInternalServerError})
		return
	}
	WriteJSONResponse(w, http.StatusOK, satellites)
}

func (s *Server) GetCachedImages(w http.ResponseWriter, r *http.Request, satelliteName string) {
	sat, err := s.dbQueries.GetSatelliteByName(r.Context(), satelliteName)
	if err != nil {
		HandleAppError(w, &AppError{Message: "satellite not found", Code: http.StatusNotFound})
		return
	}

	artifacts, err := s.dbQueries.GetLatestArtifacts(r.Context(), sat.ID)
	if err != nil {
		HandleAppError(w, &AppError{Message: "failed to get cached images", Code: http.StatusInternalServerError})
		return
	}

	WriteJSONResponse(w, http.StatusOK, artifacts)
}
