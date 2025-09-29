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
	"io"
	"net/http"
	"sync"
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
		Done: make(chan struct{}, 1),
	}
}

func (s *StatusReportingProcess) Execute(ctx context.Context, upstream chan scheduler.UpstreamInfo) error {
	s.start()
	defer s.stop()

	log := logger.FromContext(ctx).With().Str("process", s.name).Logger()

	canExecute, reason := s.CanExecute(&log)
	if !canExecute {
		log.Warn().Msgf("Process %s cannot execute: %s", s.name, reason)
		return nil
	}
	log.Info().Msgf("Executing process")

	// consume upstream info continuously
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case info, ok := <-upstream:
				if !ok {
					return
				}
				var req StatusReportParams

				c := s.cm.GetConfig()
				// todo : do it in a safe way(mutex lock)
				groundControlURL := c.AppConfig.GroundControlURL

				satteliteName, err := extractSatelliteNameFromURL(info.StateURL)
				if err != nil {
					log.Warn().Msg("Failed to parse state reporting interval")
					continue
				}

				duration, err := scheduler.ParseEveryExpr(s.cm.GetStateReportingInterval())
				if err != nil {
					log.Warn().Msg("Failed to parse state reporting interval")
					continue
				}

				// populate info from upstream channel into the request
				req.Name = satteliteName
				req.Activity = info.CurrentActivity
				req.StateReportInterval = s.cm.GetStateReportingInterval()
				req.LatestStateDigest = info.LatestStateDigest
				req.LatestConfigDigest = info.LatestConfigDigest

				// populate all other info which is not provided by the upstream channel, into the request
				collectStatusReportParams(ctx, duration, &req)

				if err := sendStatusReport(ctx, string(groundControlURL), &req); err != nil {
					log.Error().Err(err).Msg("Failed to send status report")
				}

			}
		}
	}()

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
	//todo : keep only if required
	return true, fmt.Sprintf("Process %s can execute all conditions fulfilled", s.name)

}

func (s *StatusReportingProcess) IsComplete() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cm.IsZTRDone()
}

func (s *StatusReportingProcess) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isRunning
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

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create status report request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send status report: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status report failed: %s - %s", resp.Status, string(respBody))
	}

	return nil
}
