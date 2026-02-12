package state

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	runtime "github.com/container-registry/harbor-satellite/internal/container_runtime"
	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/spiffe"
	"github.com/container-registry/harbor-satellite/pkg/config"
)

const StatusReportRoute = "satellites/sync"

type StatusReportingProcess struct {
	name             string
	isRunning        bool
	mu               *sync.Mutex
	cm               *config.ConfigManager
	spiffeClient     *spiffe.Client
	pendingCRI      []runtime.CRIConfigResult
	criReported     bool
}

func NewStatusReportingProcess(cm *config.ConfigManager) *StatusReportingProcess {
	p := &StatusReportingProcess{
		name: config.StatusReportJobName,
		mu:   &sync.Mutex{},
		cm:   cm,
	}

	if cm.IsSPIFFEEnabled() {
		spiffeCfg := cm.GetSPIFFEConfig()
		client, err := spiffe.NewClient(spiffe.Config{
			Enabled:          spiffeCfg.Enabled,
			EndpointSocket:   spiffeCfg.EndpointSocket,
			ExpectedServerID: spiffeCfg.ExpectedServerID,
		})
		if err == nil {
			p.spiffeClient = client
		}
	}

	return p
}

// SetPendingCRIResults stores CRI config results to be sent in the first heartbeat.
func (s *StatusReportingProcess) SetPendingCRIResults(results []runtime.CRIConfigResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingCRI = results
}

func (s *StatusReportingProcess) Execute(ctx context.Context) error {
	s.start()
	defer s.stop()

	log := logger.FromContext(ctx).With().Str("process", s.name).Logger()

	stateURL := s.cm.GetStateURL()
	if stateURL == "" {
		log.Warn().Msg("State URL not available yet, skipping status report")
		return nil
	}

	satelliteName, err := extractSatelliteNameFromURL(stateURL)
	if err != nil {
		log.Error().Err(err).Msg("Failed to extract satellite name from state URL")
		return err
	}

	heartbeatExpr := s.cm.GetHeartbeatInterval()
	heartbeatDuration, err := parseEveryExpr(heartbeatExpr)
	if err != nil {
		log.Warn().Err(err).Msgf("Failed to parse heartbeat interval %q, using 30s", heartbeatExpr)
		heartbeatDuration = 30 * time.Second
	}

	metricsCfg := s.cm.GetMetricsConfig()

	req := &StatusReportParams{
		Name:                satelliteName,
		StateReportInterval: heartbeatExpr,
		RequestCreatedTime:  time.Now().UTC(),
	}

	// Include pending CRI results until successfully sent
	s.mu.Lock()
	hasPendingCRI := !s.criReported && len(s.pendingCRI) > 0
	if hasPendingCRI {
		req.Activity = formatCRIActivity(s.pendingCRI)
		log.Info().Str("activity", req.Activity).Msg("Reporting CRI config results")
	}
	s.mu.Unlock()

	registryURL := s.cm.GetLocalRegistryURL()
	insecure := s.cm.UseUnsecure()
	collectStatusReportParams(ctx, heartbeatDuration, req, metricsCfg, registryURL, insecure)

	groundControlURL := s.cm.ResolveGroundControlURL()
	if err := s.sendStatusReport(ctx, groundControlURL, req); err != nil {
		log.Error().Err(err).Msg("Failed to send status report")
		return err
	}

	// Clear CRI results only after successful send
	if hasPendingCRI {
		s.mu.Lock()
		s.criReported = true
		s.pendingCRI = nil
		s.mu.Unlock()
	}

	log.Info().Str("satellite", satelliteName).Msg("Status report sent successfully")
	return nil
}

// formatCRIActivity formats CRI config results into a structured string for the Activity field.
func formatCRIActivity(results []runtime.CRIConfigResult) string {
	var parts []string
	for _, r := range results {
		status := "ok"
		if !r.Success {
			status = "err:" + r.Error
		}
		entry := string(r.CRI) + "(" + status
		if r.BackupPath != "" {
			entry += ", backup:" + r.BackupPath
		}
		entry += ")"
		parts = append(parts, entry)
	}
	return "cri_fallback_configured: " + strings.Join(parts, ", ")
}

func (s *StatusReportingProcess) sendStatusReport(ctx context.Context, groundControlURL string, req *StatusReportParams) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal status report: %w", err)
	}

	syncURL := fmt.Sprintf("%s/%s", groundControlURL, StatusReportRoute)

	var client *http.Client
	if s.spiffeClient != nil {
		if err := s.spiffeClient.Connect(ctx); err != nil {
			return fmt.Errorf("connect to SPIRE agent: %w", err)
		}
		client, err = s.spiffeClient.CreateHTTPClient()
		if err != nil {
			return fmt.Errorf("create SPIFFE HTTP client: %w", err)
		}
	} else {
		client, err = createHTTPClient(s.cm.GetTLSConfig(), s.cm.UseUnsecure())
		if err != nil {
			return fmt.Errorf("create HTTP client: %w", err)
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, syncURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Add authentication header for non-SPIFFE mode
	// SPIFFE mode uses mTLS for authentication, so no header is needed
	if s.spiffeClient == nil {
		creds := s.cm.GetSourceRegistryCredentials()
		if creds.Username != "" && creds.Password != "" {
			httpReq.SetBasicAuth(creds.Username, creds.Password)
		}
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			logger.FromContext(ctx).Warn().Err(err).Msg("error closing response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status report failed: %s", resp.Status)
	}

	return nil
}

func (s *StatusReportingProcess) Name() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.name
}

func (s *StatusReportingProcess) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isRunning
}

func (s *StatusReportingProcess) IsComplete() bool {
	return false
}

func (s *StatusReportingProcess) start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isRunning = true
}

func (s *StatusReportingProcess) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isRunning = false
}
