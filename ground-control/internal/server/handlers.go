package server

import (
	"log"
	"net/http"
	"github.com/container-registry/harbor-satellite/ground-control/pkg/version"
)

const (
	invalidNameMessage = "Invalid %s name: must be 1-255 chars, start with letter/number, and contain only lowercase letters, numbers, and ._-"
)

func (s *Server) Ping(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("pong"))
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	err := s.db.Ping()
	if err != nil {
		log.Printf("error pinging db: %v", err)
		WriteJSONResponse(w, http.StatusServiceUnavailable, map[string]string{
			"status":  "unhealthy",
			"version": version.Version,
		})
		return
	}

	WriteJSONResponse(w, http.StatusOK, map[string]string{
		"status":  "healthy",
		"version": version.Version,
	})
}
