package telemetry

import (
	"context"
	"net"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// HealthCheck reports one dependency; a non-nil error makes /healthz answer 503.
type HealthCheck func(ctx context.Context) error

type ServerOptions struct {
	Addr   string
	Checks map[string]HealthCheck
}

type Server struct{}

func NewServer(opts ServerOptions) (*Server, error) {
	return nil, notimpl.Err("0.4 telemetry server")
}

// Serve answers /healthz and /metrics on l until ctx is done.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	return notimpl.Err("0.4 /healthz and /metrics")
}

func (s *Server) Recorder() Recorder {
	return Nop{}
}
