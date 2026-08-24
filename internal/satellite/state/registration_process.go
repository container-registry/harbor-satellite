package state

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/container-registry/harbor-satellite/internal/logger"
	satTLS "github.com/container-registry/harbor-satellite/internal/satellite/tls"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/rs/zerolog"
)

type ZtrProcess struct {
	// Name is the name of the process
	name string
	// isRunning is true if the process is running
	isRunning bool
	// mu is the mutex to protect the process
	mu *sync.Mutex
	// done chan is used to communicate about the success of the ZtrProcess
	Done chan struct{}
	// Config manager to interact with the satellite config
	cm *config.ConfigManager
}

func NewZtrProcess(cm *config.ConfigManager) *ZtrProcess {
	return &ZtrProcess{
		name: config.ZTRConfigJobName,
		mu:   &sync.Mutex{},
		cm:   cm,
		Done: make(chan struct{}, 1),
	}
}

func (z *ZtrProcess) Execute(ctx context.Context) error {
	z.start()
	defer z.stop()

	log := logger.FromContext(ctx).With().Str("process", z.name).Logger()

	canExecute, reason := z.CanExecute(&log)
	if !canExecute {
		log.Warn().Msgf("Process %s cannot execute: %s", z.name, reason)
		return nil
	}
	log.Info().Msgf("Executing process")

	gcURL := z.cm.ResolveGroundControlURL()
	audit := logger.AuditFromContext(ctx)

	// Register the satellite
	stateConfig, err := registerSatellite(
		gcURL,
		z.cm.GetToken(),
		z.cm.GetTLSConfig(),
		z.cm.UseUnsecure(),
		ctx,
	)
	if err != nil {
		log.Error().Err(err).Msgf("Failed to register satellite")
		audit.Log(logger.AuditEvent{
			Operation:    logger.OpRegister,
			ResourceType: logger.ResSatellite,
			Outcome:      logger.OutcomeFailure,
			ActorType:    logger.ActorSatellite,
			Reason:       logger.ReasonRegistrationFailed,
			Details: map[string]any{
				"error":              sanitizeAuditReason(err, z.cm.GetToken()),
				"flow":               "ztr",
				"ground_control_url": gcURL,
			},
		})
		return err
	}

	if stateConfig.RegistryCredentials.Username == "" || stateConfig.RegistryCredentials.Password == "" || stateConfig.RegistryCredentials.URL == "" || stateConfig.StateURL == "" {
		log.Error().Msgf("Failed to register satellite: invalid state auth config received")
		audit.Log(logger.AuditEvent{
			Operation:    logger.OpRegister,
			ResourceType: logger.ResSatellite,
			Outcome:      logger.OutcomeFailure,
			ActorType:    logger.ActorSatellite,
			Reason:       logger.ReasonInvalidStateAuthConfig,
			Details:      map[string]any{"flow": "ztr", "ground_control_url": gcURL},
		})
		return fmt.Errorf("failed to register satellite: invalid state auth config received")
	}

	// Override Harbor registry URLs if --harbor-registry-url is set
	if override := z.cm.GetHarborRegistryURL(); override != "" {
		stateConfig, err = config.ApplyHarborRegistryOverride(stateConfig, override)
		if err != nil {
			log.Error().Err(err).Msg("Failed to apply harbor registry URL override")
			return err
		}
		log.Info().Str("override", override).Msg("Applied harbor registry URL override")
	}

	// Update the state config in app config
	z.cm.With(config.SetStateConfig(stateConfig))
	if err := z.cm.WriteConfig(); err != nil {
		log.Error().Msgf("Failed to register satellite: could not update state auth config")
		return fmt.Errorf("failed to register satellite: could not update state auth config")
	}

	audit.Log(logger.AuditEvent{
		Operation:    logger.OpRegister,
		ResourceType: logger.ResSatellite,
		Outcome:      logger.OutcomeSuccess,
		ActorType:    logger.ActorSatellite,
		Details:      map[string]any{"flow": "ztr", "ground_control_url": gcURL},
	})

	// Close the z.Done channel on successful ZTR alone.
	close(z.Done)
	return nil
}

// CanExecute checks if the process can execute.
// It returns true if the process can execute, false otherwise.
func (z *ZtrProcess) CanExecute(log *zerolog.Logger) (bool, string) {
	log.Info().Msgf("Checking if process %s can execute", z.name)

	checks := []struct {
		condition bool
		message   string
	}{
		{z.cm.GetToken() == "", "token"},
		{z.cm.ResolveGroundControlURL() == "", "ground control URL"},
	}
	var missing []string
	for _, check := range checks {
		if check.condition {
			missing = append(missing, check.message)
		}
	}
	if len(missing) > 0 {
		return false, fmt.Sprintf("missing %s, please include the required environment variables present in .env", strings.Join(missing, ", "))
	}

	return true, fmt.Sprintf("Process %s can execute all conditions fulfilled", z.name)
}

func (z *ZtrProcess) Name() string {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.name
}

func (z *ZtrProcess) IsRunning() bool {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.isRunning
}

func (z *ZtrProcess) IsComplete() bool {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.cm.IsZTRDone()
}

func (z *ZtrProcess) start() bool {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.isRunning = true
	return true
}

func (z *ZtrProcess) stop() {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.isRunning = false
}

// sanitizeAuditReason returns an error string with the satellite token redacted
// so it is safe to write to the audit log. The unredacted error is still
// available via the regular zerolog stream for debugging.
func sanitizeAuditReason(err error, token string) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if token != "" {
		s = strings.ReplaceAll(s, token, "[REDACTED]")
	}
	return s
}

func registerSatellite(groundControlURL, token string, tlsCfg config.TLSConfig, useUnsecure bool, ctx context.Context) (config.StateConfig, error) {
	httpClient, err := createHTTPClient(tlsCfg, useUnsecure)
	if err != nil {
		return config.StateConfig{}, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	client, err := groundcontrol.NewClientWithResponses(
		groundControlURL,
		groundcontrol.WithHTTPClient(httpClient),
	)
	if err != nil {
		return config.StateConfig{}, fmt.Errorf("failed to create Ground Control client: %w", err)
	}

	response, err := client.ZtrWithResponse(ctx, groundcontrol.ZTRRequest{Token: token})
	if err != nil {
		return config.StateConfig{}, fmt.Errorf("failed to send registration request: %w", err)
	}

	switch {
	case response.JSON200 != nil:
		return stateConfigFromResponse(*response.JSON200), nil
	case response.JSON400 != nil:
		return config.StateConfig{}, responseError("failed to register satellite", response.Status(), response.JSON400)
	case response.JSON401 != nil:
		return config.StateConfig{}, responseError("failed to register satellite", response.Status(), response.JSON401)
	case response.JSON422 != nil:
		return config.StateConfig{}, responseError("failed to register satellite", response.Status(), response.JSON422)
	case response.JSON429 != nil:
		return config.StateConfig{}, responseError("failed to register satellite", response.Status(), response.JSON429)
	case response.JSON500 != nil:
		return config.StateConfig{}, responseError("failed to register satellite", response.Status(), response.JSON500)
	default:
		return config.StateConfig{}, unknownResponseError("failed to register satellite", response.Status(), response.Body)
	}
}

func createHTTPClient(tlsCfg config.TLSConfig, useUnsecure bool) (*http.Client, error) {
	transport := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: true,
	}

	if useUnsecure {
		transport.TLSClientConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, //nolint:gosec // Explicitly enabled by use_unsecure.
		}
	} else if tlsCfg.CertFile != "" || tlsCfg.CAFile != "" || tlsCfg.SkipVerify {
		loadedTLSConfig, err := satTLS.LoadClientTLSConfig(&satTLS.Config{
			CertFile:   tlsCfg.CertFile,
			KeyFile:    tlsCfg.KeyFile,
			CAFile:     tlsCfg.CAFile,
			SkipVerify: tlsCfg.SkipVerify,
			MinVersion: tls.VersionTLS12,
		})
		if err != nil {
			return nil, fmt.Errorf("load TLS config: %w", err)
		}
		transport.TLSClientConfig = loadedTLSConfig
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}, nil
}
