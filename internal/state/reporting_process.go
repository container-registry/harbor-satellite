package state

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/scheduler"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
	"net/http"
	"sync"
	"time"
)

type StatusReportingProcess struct {
	name      string
	isRunning bool
	mu        *sync.Mutex
	Done      chan struct{}
	cm        *config.ConfigManager
}

func NewStatusReportingProcess(cm *config.ConfigManager) *StatusReportingProcess {
	return &StatusReportingProcess{
		name: "status_report",
		mu:   &sync.Mutex{},
		cm:   cm,
	}
}

func (s *StatusReportingProcess) Execute(ctx context.Context, upstream *scheduler.UpstreamInfo) error {
	if !s.start() {
		return nil
	}

	defer s.stop()

	log := logger.FromContext(ctx).With().Str("process", s.name).Logger()

	canExecute, reason := s.CanExecute(&log)
	if !canExecute {
		log.Warn().Msgf("Process %s cannot execute: %s", s.name, reason)
		s.stop()
		return nil
	}

	log.Info().Msg("Starting status reporting process")

	groundControlURL := s.cm.ResolveGroundControlURL()

	satelliteName, err := extractSatelliteNameFromURL(upstream.StateURL)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to extract satellite name")

	}

	duration, err := scheduler.ParseEveryExpr(s.cm.GetStateReportingInterval())
	if err != nil {
		log.Warn().Err(err).Msg("Failed to parse state reporting interval")

	}

	req := StatusReportParams{
		Name:                satelliteName,
		Activity:            upstream.CurrentActivity,
		StateReportInterval: s.cm.GetStateReportingInterval(),
		LatestStateDigest:   upstream.LatestStateDigest,
		LatestConfigDigest:  upstream.LatestConfigDigest,
	}

	if err := collectStatusReportParams(ctx, duration, &req); err != nil {
		log.Warn().Err(err).Msg("Failed to collect status report parameters")

	}

	if err := sendStatusReport(ctx, groundControlURL, &req); err != nil {
		log.Warn().Err(err).Msg("Failed to send status report")

	}

	log.Info().Msg("Heartbeat sent to ground control successfully")

	return nil
}

func (s *StatusReportingProcess) start() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isRunning = true
	return true
}

func (s *StatusReportingProcess) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isRunning = false
}

func (s *StatusReportingProcess) CanExecute(log *zerolog.Logger) (bool, string) {
	return true, fmt.Sprintf("Process %s can execute all conditions fulfilled", s.name)
}

func (s *StatusReportingProcess) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isRunning
}

func (f *StatusReportingProcess) IsComplete() bool {
	return false
}

func (s *StatusReportingProcess) Name() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.name
}

func sendStatusReport(ctx context.Context, groundControlURL string, req *StatusReportParams) error {
	url := fmt.Sprintf("%s/satellites/status", groundControlURL)

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal status report: %w", err)
	}

	// add a request level timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(timeoutCtx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create status report request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send status report: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("heartbeat request timed out : %w, with status code %d", err, resp.StatusCode)
	}

	return nil
}
