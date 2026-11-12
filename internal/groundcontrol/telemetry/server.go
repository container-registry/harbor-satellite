package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 5 * time.Second
)

// Options are the sources /metrics reads at scrape time; each may be nil.
type Options struct {
	// Pools are the database pools by plane (api, fleet, async). A pool
	// shared by several planes is reported once, under the first name.
	Pools []NamedPool
	// HeartbeatBuffer returns how many satellites this replica holds
	// unflushed heartbeats for.
	HeartbeatBuffer func() int
	Outbox          OutboxStats
}

type NamedPool struct {
	Name string
	DB   *sql.DB
}

type Server struct {
	registry *prometheus.Registry
	recorder *promRecorder
}

func NewServer(opts Options) (*Server, error) {
	registry := prometheus.NewRegistry()
	recorder := newPromRecorder()
	cs := append(recorder.collectors(),
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	seen := map[*sql.DB]bool{}
	for _, p := range opts.Pools {
		if p.DB == nil || seen[p.DB] {
			continue
		}
		seen[p.DB] = true
		cs = append(cs, collectors.NewDBStatsCollector(p.DB, p.Name))
	}
	if opts.HeartbeatBuffer != nil {
		buffer := opts.HeartbeatBuffer
		cs = append(cs, prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: MetricHeartbeatBuffer,
			Help: "Satellites with heartbeats buffered on this replica and not yet written.",
		}, func() float64 { return float64(buffer()) }))
	}
	if opts.Outbox != nil {
		cs = append(cs, newOutboxCollector(opts.Outbox))
	}
	for _, c := range cs {
		if err := registry.Register(c); err != nil {
			return nil, err
		}
	}
	return &Server{registry: registry, recorder: recorder}, nil
}

func (s *Server) Recorder() Recorder {
	return s.recorder
}

// Handler answers GET /metrics.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{}))
	return mux
}

// Serve answers /metrics on l until ctx is done.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	srv := &http.Server{
		Handler:           s.Handler(),
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
