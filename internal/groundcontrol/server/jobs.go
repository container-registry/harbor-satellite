package server

import (
	"log"
	"slices"
	"sync"
)

// stoppedJobs holds the background jobs of serve that returned while the
// server still runs. Jobs retry their own failures (ADR-0014), so a stopped
// job stays stopped until the process restarts.
type stoppedJobs struct {
	mu    sync.Mutex
	names []string
}

// JobStopped records that the background job name returned while the
// server runs; /health answers unhealthy from then on.
func (s *Server) JobStopped(name string) {
	s.stopped.mu.Lock()
	defer s.stopped.mu.Unlock()
	if !slices.Contains(s.stopped.names, name) {
		s.stopped.names = append(s.stopped.names, name)
	}
	log.Printf("background job %q stopped; /health reports unhealthy until Ground Control restarts", name)
}

// StoppedJobs returns the names JobStopped recorded, in order.
func (s *Server) StoppedJobs() []string {
	s.stopped.mu.Lock()
	defer s.stopped.mu.Unlock()
	return slices.Clone(s.stopped.names)
}
