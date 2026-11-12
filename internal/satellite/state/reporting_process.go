package state

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	runtime "github.com/container-registry/harbor-satellite/internal/satellite/container_runtime"
	"github.com/container-registry/harbor-satellite/internal/satellite/events"
	"github.com/container-registry/harbor-satellite/internal/satellite/gcclient"
	"github.com/container-registry/harbor-satellite/internal/satellite/telemetry"
	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/container-registry/harbor-satellite/internal/shared/spiffe"
	"github.com/container-registry/harbor-satellite/internal/shared/utils"
	"github.com/container-registry/harbor-satellite/pkg/config"
)

type StatusReportingProcess struct {
	name           string
	isRunning      bool
	mu             *sync.Mutex
	cm             *config.ConfigManager
	spiffeClient   *spiffe.Client
	pendingCRI     []runtime.CRIConfigResult
	eventScheduler *events.EventScheduler
	criReported    bool
	runSummary     RunSummarySource
	upload         journalUploader
	inventory      inventoryState
	recorder       telemetry.Recorder
}

func NewStatusReportingProcess(cm *config.ConfigManager, eventScheduler *events.EventScheduler) *StatusReportingProcess {
	p := &StatusReportingProcess{
		name:           config.StatusReportJobName,
		mu:             &sync.Mutex{},
		cm:             cm,
		eventScheduler: eventScheduler,
		recorder:       telemetry.Nop{},
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

// SetRecorder makes every heartbeat and journal answer show on /metrics,
// so the satellite's own view of its heartbeats can be checked.
func (s *StatusReportingProcess) SetRecorder(rec telemetry.Recorder) {
	if rec == nil {
		rec = telemetry.Nop{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recorder = rec
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

	var registryURL string
	if s.cm.GetOwnRegistry() {
		registryURL = utils.FormatRegistryURL(s.cm.GetLocalRegistryURL())
	}
	insecure := s.cm.UseUnsecure()
	collectStatusReportParams(ctx, heartbeatDuration, req, metricsCfg, registryURL, insecure)
	recordDownloadStats(req)
	s.mu.Lock()
	runSummary := s.runSummary
	s.mu.Unlock()
	if runSummary != nil {
		applyRunSummary(req, runSummary.LastRun())
	}
	report := toStatusReport(req)
	report.CredentialsGeneration = s.cm.GetStateConfig().CredentialsGeneration
	bare := s.upload.bare()
	if bare {
		req.CachedImages = nil
		report.CachedImages = nil
	}
	s.mu.Lock()
	digest := s.inventory.prepare(&report, req.CachedImages, req.CachedImages != nil)
	s.mu.Unlock()
	uploadBatch := s.cm.GetJournalConfig().UploadBatch
	batch, err := s.upload.next(uploadBatch)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to read the journal, reporting without it")
	} else if batch != nil {
		report.Journal = *batch
	}

	log.Debug().Str("ground_control_url", s.cm.ResolveGroundControlURL()).Msg("Sending status report")
	s.mu.Lock()
	recorder := s.recorder
	s.mu.Unlock()
	if recorder == nil {
		recorder = telemetry.Nop{}
	}
	resp, err := s.sendStatusReport(ctx, report)
	if err != nil {
		code := heartbeatCode(err)
		recorder.CountHeartbeat(code)
		if code == strconv.Itoa(http.StatusRequestEntityTooLarge) {
			reaction := s.upload.tooLarge(batch)
			recorder.CountHeartbeatTooLarge(reaction)
			ev := log.Error().Err(err).Str("reaction", reaction).Bool("bare", bare)
			if batch != nil {
				ev = ev.Int("journal_records", len(batch.Records))
			}
			ev.Msg("Ground Control refused the heartbeat as too large (SYNC_MAX_BODY_BYTES)")
			return err
		}
		log.Error().Err(err).Msg("Failed to send status report")
		return err
	}
	recorder.CountHeartbeat(strconv.Itoa(http.StatusOK))
	s.upload.delivered(batch, uploadBatch)
	s.mu.Lock()
	s.inventory.answered(report, digest, resp.InventoryRequested)
	s.mu.Unlock()
	if resp.Credentials != nil {
		if err := ApplyRotatedCredentials(s.cm, *resp.Credentials); err != nil {
			log.Error().Err(err).Int64("generation", resp.Credentials.Generation).Msg("Failed to apply rotated robot credentials")
		} else {
			log.Info().Int64("generation", resp.Credentials.Generation).Msg("Switched to the rotated robot credentials")
		}
	}
	if resp.Session != nil {
		s.cm.With(config.SetGCSession(config.GCSession{Token: resp.Session.Token, ExpiresAt: resp.Session.ExpiresAt}))
		if err := s.cm.WriteConfig(); err != nil {
			log.Warn().Err(err).Msg("Failed to persist the renewed Ground Control session")
		}
	}
	if batch != nil {
		outcome, err := s.upload.answer(*batch, resp.Journal)
		if outcome != "" {
			recorder.ObserveJournalAck(outcome, resp.Journal.AckedSeq)
		}
		if err != nil {
			log.Warn().Err(err).Msg("Failed to apply the journal answer")
		}
	}

	// Sending response events to eventscheduler
	for _, v := range resp.Events {
		log.Info().Msgf("Sending event: %s", v)
		s.eventScheduler.SendEvent(v)
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

// heartbeatCode is the HTTP status Ground Control refused the heartbeat
// with, or telemetry.HeartbeatTransportError when it never answered.
func heartbeatCode(err error) string {
	var statusErr *gcclient.StatusError
	switch {
	case errors.As(err, &statusErr):
		return strconv.Itoa(statusErr.StatusCode)
	case errors.Is(err, gcclient.ErrUnauthorized):
		return strconv.Itoa(http.StatusUnauthorized)
	case errors.Is(err, gcclient.ErrForbidden):
		return strconv.Itoa(http.StatusForbidden)
	case errors.Is(err, gcclient.ErrRateLimited):
		return strconv.Itoa(http.StatusTooManyRequests)
	default:
		return telemetry.HeartbeatTransportError
	}
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

// newReportClient builds the HTTP client used to send a status report,
// preferring the SPIFFE-backed client when one is configured.
func (s *StatusReportingProcess) newReportClient(ctx context.Context) (*http.Client, error) {
	if s.spiffeClient != nil {
		if err := s.spiffeClient.Connect(ctx); err != nil {
			return nil, fmt.Errorf("connect to SPIRE agent: %w", err)
		}
		client, err := s.spiffeClient.CreateHTTPClient()
		if err != nil {
			return nil, fmt.Errorf("create SPIFFE HTTP client: %w", err)
		}
		return client, nil
	}

	client, err := createHTTPClient(s.cm.GetTLSConfig(), s.cm.UseUnsecure())
	if err != nil {
		return nil, fmt.Errorf("create HTTP client: %w", err)
	}
	return client, nil
}

func (s *StatusReportingProcess) sendStatusReport(ctx context.Context, report gcclient.StatusReport) (gcclient.SyncResult, error) {
	httpClient, err := s.newReportClient(ctx)
	if err != nil {
		return gcclient.SyncResult{}, err
	}
	var creds gcclient.Credentials = gcclient.NoCredentials{}
	if s.spiffeClient == nil {
		creds = gcCredentials(s.cm)
	}
	client, err := newGCClient(s.cm, httpClient, creds)
	if err != nil {
		return gcclient.SyncResult{}, err
	}
	return client.Sync(ctx, report)
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
