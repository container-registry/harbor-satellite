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
	if stopped := s.StoppedJobs(); len(stopped) > 0 {
		resp.Status = Unhealthy
		resp.Components = map[string]ComponentHealth{}
		for _, name := range stopped {
			resp.Components[name] = ComponentHealth{Status: "unhealthy", Detail: "stopped"}
		}
	}
	if s.harborContact != nil {
		harbor := ComponentHealth{Status: "healthy"}
		last := s.harborContact.LastContact()
		switch {
		case last.IsZero():
			harbor = ComponentHealth{Status: "degraded", Detail: "no contact since startup"}
			resp.Status = worse(resp.Status, Degraded)
		case time.Since(last) > s.harborStaleAfter:
			harbor = ComponentHealth{Status: "degraded", Detail: "no recent contact", LastContact: last}
			resp.Status = worse(resp.Status, Degraded)
		default:
			harbor.LastContact = last
		}
		if resp.Components == nil {
			resp.Components = map[string]ComponentHealth{}
		}
		resp.Components["harbor"] = harbor
	}
	status := http.StatusOK
	if resp.Status == Unhealthy {
		status = http.StatusServiceUnavailable
	}
	WriteJSONResponse(w, status, resp)
}

// worse returns the less healthy of a and b.
func worse(a, b HealthResponseStatus) HealthResponseStatus {
	if a == Unhealthy || b == Unhealthy {
		return Unhealthy
	}
	if a == Degraded || b == Degraded {
		return Degraded
	}
	return Healthy
}
