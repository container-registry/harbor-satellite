package server

import (
	"log"
	"net/http"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// RevokeSatellite serves POST /api/satellites/{satellite}/revoke (plan
// item 4.3): the satellite keeps its sessions and gets 403 on sync and
// ZTR, and the reconciler disables its robot.
func (s *Server) RevokeSatellite(w http.ResponseWriter, r *http.Request, satellite string) {
	view, err := s.svc().Satellites.Revoke(r.Context(), satellite)
	s.writeLifecycle(w, r, "revoke", satellite, view, err)
}

// UnrevokeSatellite serves POST /api/satellites/{satellite}/unrevoke; the
// satellite is pending until its next heartbeat.
func (s *Server) UnrevokeSatellite(w http.ResponseWriter, r *http.Request, satellite string) {
	view, err := s.svc().Satellites.Unrevoke(r.Context(), satellite)
	s.writeLifecycle(w, r, "unrevoke", satellite, view, err)
}

// GetSatelliteLifecycle serves GET /api/satellites/{satellite}/lifecycle:
// the state of ADR-0006 and the convergence per state ref.
func (s *Server) GetSatelliteLifecycle(w http.ResponseWriter, r *http.Request, satellite string) {
	view, err := s.svc().Satellites.View(r.Context(), satellite)
	s.writeLifecycle(w, r, "", satellite, view, err)
}

func (s *Server) writeLifecycle(w http.ResponseWriter, _ *http.Request, op, satellite string, view fleet.SatelliteView, err error) {
	if err != nil {
		if StatusForError(err) == http.StatusInternalServerError {
			log.Printf("satellite %s lifecycle %s: %v", satellite, op, err)
		}
		handleFleetError(w, err, nil)
		return
	}
	if op != "" {
		log.Printf("satellite %s: %s, now %s", satellite, op, view.State)
	}
	WriteJSONResponse(w, http.StatusOK, lifecycleResponse(view))
}

func lifecycleResponse(v fleet.SatelliteView) SatelliteLifecycleResponse {
	applied := make(map[string]fleet.Digest, len(v.AppliedStates))
	for _, a := range v.AppliedStates {
		applied[a.Ref] = a.Digest
	}
	states := make([]StateConvergence, 0, len(v.PublishedGroups))
	for _, g := range v.PublishedGroups {
		states = append(states, StateConvergence{Ref: g.Ref, Digest: string(applied[g.Ref]), PublishedDigest: string(g.ArtifactDigest)})
	}
	return SatelliteLifecycleResponse{
		Name:                    v.Satellite.Name,
		State:                   string(v.State),
		LastSeen:                v.Satellite.LastSeen,
		AppliedStateDigest:      string(v.AppliedStateDigest),
		AppliedConfigDigest:     string(v.AppliedConfigDigest),
		AppliedStates:           states,
		Converged:               v.Converged(),
		DesiredVersion:          v.DesiredVersion,
		RenderedDigest:          string(v.RenderedDigest),
		PublishedArtifactDigest: string(v.PublishedArtifactDigest),
		PublishError:            v.PublishError,
		LastReplicationError:    v.LastReplicationError,
		RevokedAt:               v.RevokedAt,
		UpdatedAt:               v.UpdatedAt,
	}
}

func nullableString(s string) NullableStringValue {
	return NullableStringValue{String: s, Valid: s != ""}
}

func nullableTime(t time.Time) NullableTimeValue {
	return NullableTimeValue{Time: t, Valid: !t.IsZero()}
}

// ListActiveSatellites and ListStaleSatellites read satellite_view (plan
// item 4.3): online is what heartbeats keep alive, stale what the stale
// detector marked.
func (s *Server) ListActiveSatellites(w http.ResponseWriter, r *http.Request) {
	views, err := s.svc().Satellites.ListByState(r.Context(), fleet.StateOnline)
	if err != nil {
		log.Printf("Failed to get active satellites: %v", err)
		handleFleetError(w, err, &AppError{Message: "failed to get active satellites", Code: StatusForError(err)})
		return
	}
	out := make([]ActiveSatelliteResponse, 0, len(views))
	for _, v := range views {
		statusTime := v.LastReportedAt
		if statusTime.IsZero() {
			statusTime = v.Satellite.LastSeen
		}
		out = append(out, ActiveSatelliteResponse{
			ID: int32(v.Satellite.ID), Name: v.Satellite.Name, CreatedAt: v.Satellite.CreatedAt, UpdatedAt: v.Satellite.UpdatedAt,
			LastSeen: nullableTime(v.Satellite.LastSeen), HeartbeatInterval: nullableString(v.Satellite.HeartbeatInterval),
			LastActivity: v.LastActivity, LastStatusTime: statusTime,
		})
	}
	WriteJSONResponse(w, http.StatusOK, out)
}

func (s *Server) ListStaleSatellites(w http.ResponseWriter, r *http.Request) {
	views, err := s.svc().Satellites.ListByState(r.Context(), fleet.StateStale)
	if err != nil {
		log.Printf("Failed to get stale satellites: %v", err)
		handleFleetError(w, err, &AppError{Message: "failed to get stale satellites", Code: StatusForError(err)})
		return
	}
	now := time.Now()
	out := make([]StaleSatelliteResponse, 0, len(views))
	for _, v := range views {
		out = append(out, StaleSatelliteResponse{
			ID: int32(v.Satellite.ID), Name: v.Satellite.Name, CreatedAt: v.Satellite.CreatedAt, UpdatedAt: v.Satellite.UpdatedAt,
			LastSeen: nullableTime(v.Satellite.LastSeen), HeartbeatInterval: nullableString(v.Satellite.HeartbeatInterval),
			SecondsSinceSeen: int64(now.Sub(v.Satellite.LastSeen).Seconds()),
		})
	}
	WriteJSONResponse(w, http.StatusOK, out)
}
