package state

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/internal/registry"
	"github.com/container-registry/harbor-satellite/pkg/config"
)

// ProxyCacheSetupProcess is a one-shot scheduler process that runs after ZTR
// completes. It writes the sync credentials file and updates the Zot config
// with the sync extension for on-demand proxy-cache mode.
type ProxyCacheSetupProcess struct {
	name                string
	isRunning           bool
	done                bool
	mu                  *sync.Mutex
	cm                  *config.ConfigManager
	syncCredentialsFile string
	zotTempConfigPath   string
}

// NewProxyCacheSetupProcess creates a new one-shot process for configuring
// Zot's sync extension after ZTR provides Harbor credentials.
func NewProxyCacheSetupProcess(cm *config.ConfigManager, syncCredentialsFile, zotTempConfigPath string) *ProxyCacheSetupProcess {
	return &ProxyCacheSetupProcess{
		name:                config.ProxyCacheSetupJobName,
		mu:                  &sync.Mutex{},
		cm:                  cm,
		syncCredentialsFile: syncCredentialsFile,
		zotTempConfigPath:   zotTempConfigPath,
	}
}

func (p *ProxyCacheSetupProcess) Execute(ctx context.Context) error {
	p.start()
	defer p.stop()

	log := logger.FromContext(ctx).With().Str("process", p.name).Logger()

	// Wait for ZTR to complete (credentials must be available)
	if !p.cm.IsZTRDone() {
		log.Debug().Msg("ZTR not done yet, skipping proxy-cache setup")
		return nil
	}

	log.Info().Msg("Configuring Zot sync extension for proxy-cache mode")

	upstreamURL := p.cm.GetSourceRegistryURL()
	username := p.cm.GetSourceRegistryUsername()
	password := p.cm.GetSourceRegistryPassword()

	if upstreamURL == "" || username == "" || password == "" {
		log.Error().Msg("Missing upstream registry credentials for proxy-cache setup")
		return nil
	}

	// Write the sync credentials file
	if err := registry.WriteSyncCredentialsFile(p.syncCredentialsFile, upstreamURL, username, password); err != nil {
		log.Error().Err(err).Msg("Write sync credentials file")
		return err
	}
	log.Info().Str("path", p.syncCredentialsFile).Msg("Wrote sync credentials file")

	// Build the proxy-cache Zot config with sync extension
	baseConfig := p.cm.GetRawZotConfig()
	pollInterval := p.cm.GetSyncPollInterval()

	proxyCacheConfig, err := registry.BuildProxyCacheZotConfig(registry.ProxyCacheParams{
		BaseConfig:      baseConfig,
		UpstreamURL:     upstreamURL,
		CredentialsFile: p.syncCredentialsFile,
		PollInterval:    pollInterval,
		UseUnsecure:     p.cm.UseUnsecure(),
	})
	if err != nil {
		log.Error().Err(err).Msg("Build proxy-cache Zot config")
		return err
	}

	// Update the config manager, which triggers hot-reload into the running Zot
	p.cm.With(config.SetZotConfigRaw(json.RawMessage(proxyCacheConfig)))

	log.Info().Msg("Proxy-cache Zot config applied, sync extension enabled")

	p.mu.Lock()
	p.done = true
	p.mu.Unlock()

	return nil
}

func (p *ProxyCacheSetupProcess) Name() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.name
}

func (p *ProxyCacheSetupProcess) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.isRunning
}

// IsComplete returns true after the sync config has been applied.
// The scheduler will stop running this process once complete.
func (p *ProxyCacheSetupProcess) IsComplete() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.done
}

func (p *ProxyCacheSetupProcess) start() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.isRunning = true
}

func (p *ProxyCacheSetupProcess) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.isRunning = false
}
