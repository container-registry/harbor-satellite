package server

import "net/http"

// RefreshSatelliteCredentials serves POST /satellites/refresh. Its
// operationId differs from the older RefreshSatellite handler, which has the
// same signature and stays unrouted until this one replaces it.
func (s *Server) RefreshSatelliteCredentials(w http.ResponseWriter, _ *http.Request) {
	notImplemented(w, "D2 refresh satellite credentials")
}
