package server

import (
	"net/http/pprof"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type MetricsRegistrar struct{}

type DebugRegistrar struct{}

func (m *MetricsRegistrar) RegisterRoutes(router Router) {
	metricsGroup := router.Group("/metrics")
	metricsGroup.Handle("", promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{}))
}

func (dr *DebugRegistrar) RegisterRoutes(router Router) {
	debugGroup := router.Group("/debug/pprof")
	debugGroup.HandleFunc("/", pprof.Index)
	debugGroup.HandleFunc("/profile", pprof.Profile)
	debugGroup.HandleFunc("/trace", pprof.Trace)
	debugGroup.HandleFunc("/symbol", pprof.Symbol)
	debugGroup.Handle("/heap", pprof.Handler("heap"))
	debugGroup.Handle("/goroutine", pprof.Handler("goroutine"))
	debugGroup.Handle("/threadcreate", pprof.Handler("threadcreate"))
	debugGroup.Handle("/block", pprof.Handler("block"))
	debugGroup.Handle("/mutex", pprof.Handler("mutex"))
}
