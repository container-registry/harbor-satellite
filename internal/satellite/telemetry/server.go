package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	checkTimeout      = 2 * time.Second
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 5 * time.Second
)

// HealthCheck reports one dependency; a non-nil error makes /healthz answer 503.
type HealthCheck func(ctx context.Context) error

type ServerOptions struct {
	Addr   string
	Checks map[string]HealthCheck
}

type Server struct {
	checksMu sync.RWMutex
	checks   map[string]HealthCheck
	registry *prometheus.Registry
	recorder *promRecorder
}

// AddCheck registers a check after construction, for dependencies that only
// exist once the recorder is already in use; it replaces one with that name.
func (s *Server) AddCheck(name string, check HealthCheck) error {
	if name == "" || check == nil {
		return errors.New("telemetry: a health check needs a name and a function")
	}
	s.checksMu.Lock()
	defer s.checksMu.Unlock()
	s.checks[name] = check
	return nil
}

func NewServer(opts ServerOptions) (*Server, error) {
	for name, check := range opts.Checks {
		if name == "" || check == nil {
			return nil, errors.New("telemetry: every health check needs a name and a function")
		}
	}
	registry := prometheus.NewRegistry()
	recorder := newPromRecorder()
	if err := recorder.register(registry); err != nil {
		return nil, err
	}
	if err := registry.Register(collectors.NewGoCollector()); err != nil {
		return nil, err
	}
	if err := registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})); err != nil {
		return nil, err
	}
	checks := make(map[string]HealthCheck, len(opts.Checks))
	for name, check := range opts.Checks {
		checks[name] = check
	}
	return &Server{checks: checks, registry: registry, recorder: recorder}, nil
}

// Serve answers /healthz and /metrics on l until ctx is done.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.Handle("GET /metrics", promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{}))
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(l) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Recorder() Recorder {
	return s.recorder
}

type healthReport struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), checkTimeout)
	defer cancel()

	s.checksMu.RLock()
	checks := make(map[string]HealthCheck, len(s.checks))
	for name, check := range s.checks {
		checks[name] = check
	}
	s.checksMu.RUnlock()
	names := make([]string, 0, len(checks))
	for name := range checks {
		names = append(names, name)
	}
	sort.Strings(names)

	results := make([]string, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() {
			if err := checks[name](ctx); err != nil {
				results[i] = err.Error()
				return
			}
			results[i] = "ok"
		})
	}
	wg.Wait()

	report := healthReport{Status: "ok", Checks: make(map[string]string, len(names))}
	status := http.StatusOK
	for i, name := range names {
		report.Checks[name] = results[i]
		if results[i] != "ok" {
			report.Status = "unhealthy"
			status = http.StatusServiceUnavailable
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(report); err != nil {
		logger.FromContext(r.Context()).Debug().Err(err).Msg("write /healthz response")
	}
}
