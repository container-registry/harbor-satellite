package satellite

import (
	"context"
	"errors"
	"net"
	"net/http"

	runtime "github.com/container-registry/harbor-satellite/internal/satellite/container_runtime"
	"github.com/container-registry/harbor-satellite/internal/satellite/events"
	"github.com/container-registry/harbor-satellite/internal/satellite/peer"
	"github.com/container-registry/harbor-satellite/internal/satellite/scheduler"
	"github.com/container-registry/harbor-satellite/internal/satellite/state"
	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/container-registry/harbor-satellite/pkg/config"
)

type Satellite struct {
	cm             *config.ConfigManager
	criResults     []runtime.CRIConfigResult
	schedulers     []*scheduler.Scheduler
	stateFilePath  string
	storeRoot      string
	stateProcess   *state.FetchAndReplicateStateProcess
	eventscheduler *events.EventScheduler
	peerListen     string
	peerURLs       []string
	peerServer     *http.Server
}

func NewSatellite(cm *config.ConfigManager, criResults []runtime.CRIConfigResult, stateFilePath, storeRoot string, jq *events.EventScheduler) *Satellite {
	return &Satellite{
		cm:             cm,
		criResults:     criResults,
		schedulers:     make([]*scheduler.Scheduler, 0),
		stateFilePath:  stateFilePath,
		storeRoot:      storeRoot,
		eventscheduler: jq,
	}
}

// ConfigurePeer sets the replica-proxy bind address and the static peer allow-list.
// listen is this satellite's registry bind (ADR-0009 replica GET/HEAD). urls are
// other satellites' replica-proxy URLs in the same Ground Control group;
// empty urls means Harbor only. Out-of-group URLs must not be listed.
func (s *Satellite) ConfigurePeer(listen string, urls []string) {
	s.peerListen = listen
	s.peerURLs = append([]string(nil), urls...)
}

func (s *Satellite) Run(ctx context.Context) error {
	log := logger.FromContext(ctx)
	log.Info().Msg("Starting Satellite")

	fetchAndReplicateStateProcess := state.NewFetchAndReplicateStateProcess(s.cm, s.stateFilePath, s.storeRoot, log)
	s.stateProcess = fetchAndReplicateStateProcess

	if err := s.startReplicaProxy(ctx, fetchAndReplicateStateProcess); err != nil {
		return err
	}

	// Create ZTR scheduler if not already done
	if !s.cm.IsZTRDone() {
		var ztrScheduler *scheduler.Scheduler
		var err error

		if s.cm.IsSPIFFEEnabled() {
			log.Info().Msg("SPIFFE authentication enabled, using SPIFFE-based ZTR")
			spiffeZtrProcess, processErr := state.NewSpiffeZtrProcess(s.cm)
			if processErr != nil {
				log.Error().Err(processErr).Msg("Failed to create SPIFFE ZTR process")
				return processErr
			}
			ztrScheduler, err = scheduler.NewSchedulerWithInterval(
				s.cm.GetRegistrationInterval(),
				spiffeZtrProcess,
				log,
			)
		} else {
			log.Info().Msg("Using token-based ZTR")
			ztrProcess := state.NewZtrProcess(s.cm)
			ztrScheduler, err = scheduler.NewSchedulerWithInterval(
				s.cm.GetRegistrationInterval(),
				ztrProcess,
				log,
			)
		}

		if err != nil {
			log.Error().Err(err).Msg("Failed to create ZTR scheduler")
			return err
		}
		s.schedulers = append(s.schedulers, ztrScheduler)
		ztrScheduler.Start(ctx)
	}

	// Create state replication scheduler
	stateScheduler, err := scheduler.NewSchedulerWithInterval(
		s.cm.GetStateReplicationInterval(),
		fetchAndReplicateStateProcess,
		log,
	)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create state replication scheduler")
		return err
	}
	s.schedulers = append(s.schedulers, stateScheduler)
	stateScheduler.Start(ctx)

	// Create status report scheduler with pending CRI results
	statusReportProcess := state.NewStatusReportingProcess(s.cm, s.eventscheduler)
	if len(s.criResults) > 0 {
		statusReportProcess.SetPendingCRIResults(s.criResults)
	}
	statusScheduler, err := scheduler.NewSchedulerWithInterval(
		s.cm.GetHeartbeatInterval(),
		statusReportProcess,
		log,
	)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create status report scheduler")
		return err
	}
	s.schedulers = append(s.schedulers, statusScheduler)
	statusScheduler.Start(ctx)

	// Registering events
	log.Info().Msg("registering events")
	err = s.registerEvents(context.Background(), s.cm)
	if err != nil {
		return err
	}

	return ctx.Err()
}

func (s *Satellite) startReplicaProxy(ctx context.Context, process *state.FetchAndReplicateStateProcess) error {
	log := logger.FromContext(ctx)
	if s.cm.GetOwnRegistry() {
		if s.peerListen != "" || len(s.peerURLs) > 0 {
			log.Warn().Msg("Peer distribution is skipped in BYO registry mode")
		}
		return nil
	}
	if s.storeRoot == "" {
		return nil
	}

	ociStore, err := store.NewOCIStore(s.storeRoot, store.RegistryOptions{})
	if err != nil {
		return err
	}
	process.SetOCIStore(ociStore)
	process.SetPeerURLs(s.peerURLs)

	if s.peerListen == "" {
		return nil
	}

	addr := peer.NormalizeAddr(s.peerListen)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.peerServer = peer.NewHTTPServer(addr, ociStore.Layout())
	log.Info().Str("addr", listener.Addr().String()).Msg("Replica proxy listening")
	go func() {
		if serveErr := s.peerServer.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Error().Err(serveErr).Msg("Replica proxy server failed")
		}
	}()
	return nil
}

func (s *Satellite) GetSchedulers() []*scheduler.Scheduler {
	return s.schedulers
}

// PersistState writes the current in-memory state to disk.
// Called during graceful shutdown to ensure no state is lost.
func (s *Satellite) PersistState() error {
	if s.stateProcess == nil {
		return nil
	}
	return s.stateProcess.PersistState()
}

// Stop gracefully stops all schedulers and logs the shutdown process.
func (s *Satellite) Stop(ctx context.Context) {
	log := logger.FromContext(ctx)
	if s.peerServer != nil {
		if err := s.peerServer.Shutdown(ctx); err != nil {
			log.Warn().Err(err).Msg("Replica proxy shutdown failed")
		}
	}
	log.Info().Int("scheduler_count", len(s.schedulers)).
		Msg("Initiating scheduler shutdown")

	failedCount := 0
	for i, sched := range s.schedulers {
		log.Debug().
			Int("index", i).
			Str("scheduler", sched.Name()).
			Msg("Stopping scheduler")
		if err := sched.Stop(ctx); err != nil {
			failedCount++
			log.Warn().Err(err).Str("scheduler", sched.Name()).Msg("Scheduler stop failed")
		}
	}

	if failedCount > 0 {
		log.Warn().Int("failed_count", failedCount).Msg("Some schedulers failed to stop")
	} else {
		log.Info().Msg("All schedulers stopped")
	}
}

// Registers actions that the Job Queue understands
// and executes
func (s *Satellite) registerEvents(ctx context.Context, cm *config.ConfigManager) error {
	log := logger.FromContext(ctx)
	var errs []error

	// Create Event Schedulers
	refreshSched, err := events.NewRefreshCredentialsEvent(cm, log)
	if err != nil {
		errs = append(errs, err)
	}

	// Register Schedulers
	s.eventscheduler.Register(ctx, refreshSched)

	return errors.Join(errs...)
}
