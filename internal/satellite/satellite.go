package satellite

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/scheduler"
	"github.com/container-registry/harbor-satellite/internal/state"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
)

type Satellite struct {
	cm *config.ConfigManager
}

func NewSatellite(cm *config.ConfigManager) *Satellite {
	return &Satellite{
		cm: cm,
	}
}


func (s *Satellite) Run(ctx context.Context) error {
	log := logger.FromContext(ctx)
	log.Info().Msg("Starting Satellite")

	fetchAndReplicateStateProcess := state.NewFetchAndReplicateStateProcess(s.cm)
	ztrProcess := state.NewZtrProcess(s.cm)
	path := "satellites/status"

	tokenStatusURL := fmt.Sprintf("%s/%s/%s", s.cm.ResolveGroundControlURL(), path, s.cm.Token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenStatusURL, nil)

	if err != nil {
		log.Error().Err(err).Msg("Failed to create request for token status")
		return err
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Error().Err(err).Msg("Failed to check token status")
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to register satellite: %s", response.Status)
	}
	
	var respBody struct {
		TokenConsumed bool `json:"token_consumed"`
	}
	
	if err := json.NewDecoder(response.Body).Decode(&respBody); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	
	if !respBody.TokenConsumed {
		// schedule ztr
		go ScheduleFunc(ctx, log, s.cm.GetRegistrationInterval(), ztrProcess)
	}

	// schedule state replication
	go ScheduleFunc(ctx, log, s.cm.GetStateReplicationInterval(), fetchAndReplicateStateProcess)

	return ctx.Err()
}

// TODO: lets pass the ticker directly to the scheduler. We can reset the ticker which streamlines everything.
func ScheduleFunc(ctx context.Context, log *zerolog.Logger, interval string, process scheduler.Process) {
	duration, _ := parseEveryExpr(interval)
	ticker := time.NewTicker(duration)
	defer ticker.Stop()

	log.Info().Str("Process", process.Name()).Msgf("Task will be performed at every %s", interval)

	// Run once immediately
	launchProcess(ctx, log, process)

	for {
		select {
		case <-ctx.Done():
			log.Info().Str("Process", process.Name()).Msg("Scheduler received cancellation signal. Exiting...")
			return
		case <-ticker.C:
			if process.IsComplete() {
				log.Info().Str("Process", process.Name()).Msg("Process marked as complete. Stopping scheduling.")
				return
			}
			launchProcess(ctx, log, process)
		}
	}
}

func launchProcess(ctx context.Context, log *zerolog.Logger, process scheduler.Process) {
	if !process.IsRunning() {
		log.Info().Str("Process", process.Name()).Msg("Scheduler triggering task execution")
		go func() {
			if err := process.Execute(ctx); err != nil {
				log.Warn().Str("Process", process.Name()).Err(err).Msg("Error occurred while executing process.")
			}
		}()
	} else {
		log.Debug().Str("Process", process.Name()).Msg("Process already executing")
	}
}

func parseEveryExpr(expr string) (time.Duration, error) {
	const prefix = "@every "
	if expr == "" {
		return 0, fmt.Errorf("empty expression provided")
	}

	if !strings.HasPrefix(expr, prefix) {
		return 0, fmt.Errorf("unsupported format: must start with %q", prefix)
	}
	return time.ParseDuration(strings.TrimPrefix(expr, prefix))
}
