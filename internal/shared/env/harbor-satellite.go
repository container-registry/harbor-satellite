package env

import (
	"time"

	"github.com/container-registry/harbor-satellite/pkg/config"
)

type HarborSatellite struct {
	Token                  string           `env:"TOKEN"`
	GroundControlURL       string           `env:"GROUND_CONTROL_URL"`
	SPIFFEEnabled          bool             `env:"SPIFFE_ENABLED"            envDefault:"false"`
	SPIFFEEndpointSocket   string           `env:"SPIFFE_ENDPOINT_SOCKET"    envDefault:"unix:///run/spire/sockets/agent.sock"`
	SPIFFEExpectedServerID string           `env:"SPIFFE_EXPECTED_SERVER_ID"`
	UseUnsecure            bool             `env:"USE_UNSECURE"              envDefault:"false"`
	BYORegistry            bool             `env:"BYO_REGISTRY"              envDefault:"false"`
	RegistryURL            string           `env:"REGISTRY_URL"`
	RegistryUsername       string           `env:"REGISTRY_USERNAME"`
	RegistryPassword       string           `env:"REGISTRY_PASSWORD"`
	ConfigDir              string           `env:"CONFIG_DIR"`
	DesiredStateFile       string           `env:"DESIRED_STATE_FILE"`
	RegistryDataDir        string           `env:"REGISTRY_DATA_DIR"`
	ShutdownTimeout        string           `env:"SHUTDOWN_TIMEOUT"          envDefault:"30s"`
	NoRegistryFallback     bool             `env:"NO_REGISTRY_FALLBACK"      envDefault:"false"`
	HarborRegistryURL      string           `env:"HARBOR_REGISTRY_URL"`
	DirectDelivery         bool             `env:"DIRECT_DELIVERY"           envDefault:"false"`
	ImageDir               string           `env:"IMAGE_DIR"`
	ProxyMode              config.ProxyMode `env:"PROXY_MODE"                envDefault:"proxy"`
	ProxyPort              int              `env:"PROXY_PORT"                envDefault:"8585"`
	TelemetryAddr          string           `env:"TELEMETRY_ADDR"            envDefault:"127.0.0.1:8586"`
	StoreGCGrace           time.Duration    `env:"STORE_GC_GRACE"            envDefault:"10m"`

	ProxyMaxConnections      int           `env:"PROXY_MAX_CONNECTIONS"      envDefault:"0"`
	ProxyWriteTimeout        time.Duration `env:"PROXY_WRITE_TIMEOUT"        envDefault:"60s"`
	ProxyStreaming           bool          `env:"PROXY_STREAMING"            envDefault:"false"`
	ProxyPrefetchConcurrency int           `env:"PROXY_PREFETCH_CONCURRENCY" envDefault:"2"`
}

func (h HarborSatellite) ApplyDefaults() HarborSatellite {
	if h.SPIFFEEndpointSocket == "" {
		h.SPIFFEEndpointSocket = config.DefaultSPIFFEEndpointSocket
	}
	if h.ShutdownTimeout == "" {
		h.ShutdownTimeout = "30s"
	}
	if h.ProxyMode == "" {
		h.ProxyMode = config.ProxyModeProxy
	}
	return h
}
