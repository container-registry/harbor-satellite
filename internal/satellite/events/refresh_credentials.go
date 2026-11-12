package events

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/satellite/gcclient"
	"github.com/container-registry/harbor-satellite/internal/satellite/scheduler"
	"github.com/container-registry/harbor-satellite/internal/shared/spiffe"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
)

type RefreshCredentialProcess struct {
	name         string
	isRunning    bool
	isComplete   bool
	cm           *config.ConfigManager
	log          *zerolog.Logger
	spiffeClient *spiffe.Client

	mu sync.RWMutex
}

func NewRefreshCredentialsEvent(cm *config.ConfigManager, log *zerolog.Logger) (*scheduler.Scheduler, error) {
	proc := &RefreshCredentialProcess{
		name:       "refresh_credentials",
		isRunning:  false,
		isComplete: false,
		cm:         cm,
		log:        log,
	}

	if cm.IsSPIFFEEnabled() {
		spiffeCfg := cm.GetSPIFFEConfig()
		client, err := spiffe.NewClient(spiffe.Config{
			Enabled:          spiffeCfg.Enabled,
			EndpointSocket:   spiffeCfg.EndpointSocket,
			ExpectedServerID: spiffeCfg.ExpectedServerID,
		})
		if err == nil {
			proc.spiffeClient = client
		}
	}

	sched, err := scheduler.NewScheduler(proc, log)
	if err != nil {
		return nil, err
	}

	return sched, nil
}

func (s *RefreshCredentialProcess) Execute(ctx context.Context) error {
	s.start()
	defer s.stop()

	if s.cm == nil {
		return fmt.Errorf("config manager not found")
	}

	client, err := s.newClient(ctx)
	if err != nil {
		return err
	}
	res, err := client.RefreshCredentials(ctx)
	if err != nil {
		return err
	}

	s.cm.With(config.SetStateAuth(s.cm.GetSourceRegistryUsername(), res.Secret, config.URL(s.cm.GetSourceRegistryURL())))
	if err := s.cm.WriteConfig(); err != nil {
		return fmt.Errorf("persist refreshed credentials: %w", err)
	}
	s.log.Info().Msg("Refreshed the registry robot secret")
	return nil
}

// newClient authenticates with the SVID when SPIFFE is on, otherwise with
// the robot account whose secret is being rotated.
func (s *RefreshCredentialProcess) newClient(ctx context.Context) (*gcclient.Client, error) {
	var (
		httpClient *http.Client
		creds      gcclient.Credentials
		err        error
	)
	if s.spiffeClient != nil {
		if err := s.spiffeClient.Connect(ctx); err != nil {
			return nil, fmt.Errorf("connect to SPIRE agent: %w", err)
		}
		if httpClient, err = s.spiffeClient.CreateHTTPClient(); err != nil {
			return nil, fmt.Errorf("create SPIFFE HTTP client: %w", err)
		}
		creds = gcclient.NoCredentials{}
	} else {
		if httpClient, err = createHTTPClient(s.cm.GetTLSConfig(), s.cm.UseUnsecure()); err != nil {
			return nil, fmt.Errorf("create HTTP client: %w", err)
		}
		creds = gcclient.RobotBasic(func() (string, string) {
			return s.cm.GetSourceRegistryUsername(), s.cm.GetSourceRegistryPassword()
		})
	}
	return gcclient.New(gcclient.Options{
		BaseURL:       s.cm.ResolveGroundControlURL(),
		HTTPClient:    httpClient,
		Credentials:   creds,
		AllowInsecure: s.cm.UseUnsecure(),
	})
}

func (s *RefreshCredentialProcess) Name() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.name
}

func (s *RefreshCredentialProcess) IsComplete() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isComplete
}

func (s *RefreshCredentialProcess) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isRunning
}

func (s *RefreshCredentialProcess) start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isRunning = true
}

func (s *RefreshCredentialProcess) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isRunning = false
}
