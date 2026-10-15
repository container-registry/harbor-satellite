package server

import "net/http"

// RevokeSatellite serves POST /api/satellites/{satellite}/revoke (plan
// item 4.3): the satellite keeps its sessions and gets 403 on sync and
// ZTR, and the reconciler disables its robot.
func (s *Server) RevokeSatellite(w http.ResponseWriter, _ *http.Request, _ string) {
	notImplemented(w, "4.3 revoke satellite")
}

// UnrevokeSatellite serves POST /api/satellites/{satellite}/unrevoke; the
// satellite is pending until its next heartbeat.
func (s *Server) UnrevokeSatellite(w http.ResponseWriter, _ *http.Request, _ string) {
	notImplemented(w, "4.3 unrevoke satellite")
}

// GetSatelliteLifecycle serves GET /api/satellites/{satellite}/lifecycle:
// the state of ADR-0006 and the convergence per state ref.
func (s *Server) GetSatelliteLifecycle(w http.ResponseWriter, _ *http.Request, _ string) {
	notImplemented(w, "4.3 satellite lifecycle")
}
