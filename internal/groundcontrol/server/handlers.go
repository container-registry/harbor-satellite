package server

import (
	"log"
	"net/http"
	"time"
)

const (
	invalidNameMessage = "Invalid %s name: must be 1-255 chars, start with letter/number, and contain only lowercase letters, numbers, and ._-"
)

func (s *Server) Ping(w http.ResponseWriter, r *http.Request) {
	if _, err := w.Write([]byte("pong")); err != nil {
		log.Printf("failed to write ping response: %v", err)
	}
}

func (s *Server) Health(w http.ResponseWriter, _ *http.Request) {
	err := s.db.Ping()
	if err != nil {
		log.Printf("error pinging db: %v", err)
		WriteJSONResponse(w, http.StatusServiceUnavailable, HealthResponse{Status: Unhealthy})
		return
	}

	resp := HealthResponse{Status: Healthy}
	if s.harborContact != nil {
		harbor := ComponentHealth{Status: "healthy"}
		last := s.harborContact.LastContact()
		switch {
		case last.IsZero():
			harbor = ComponentHealth{Status: "degraded", Detail: "no contact since startup"}
			resp.Status = Degraded
		case time.Since(last) > s.harborStaleAfter:
			harbor = ComponentHealth{Status: "degraded", Detail: "no recent contact", LastContact: last}
			resp.Status = Degraded
		default:
			harbor.LastContact = last
		}
		resp.Components = map[string]ComponentHealth{"harbor": harbor}
	}
	WriteJSONResponse(w, http.StatusOK, resp)
}
