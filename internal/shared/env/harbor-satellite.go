package env

import (
	"github.com/container-registry/harbor-satellite/pkg/config"
)

type HarborSatellite struct {
	Token                  string `env:"TOKEN"`
	GroundControlURL       string `env:"GROUND_CONTROL_URL"`
	SPIFFEEnabled          bool   `env:"SPIFFE_ENABLED"            envDefault:"false"`
	SPIFFEEndpointSocket   string `env:"SPIFFE_ENDPOINT_SOCKET"    envDefault:"unix:///run/spire/sockets/agent.sock"`
	SPIFFEExpectedServerID string `env:"SPIFFE_EXPECTED_SERVER_ID"`
	UseUnsecure            bool   `env:"USE_UNSECURE"              envDefault:"false"`
	BYORegistry            bool   `env:"BYO_REGISTRY"              envDefault:"false"`
	RegistryURL            string `env:"REGISTRY_URL"`
	RegistryUsername       string `env:"REGISTRY_USERNAME"`
	RegistryPassword       string `env:"REGISTRY_PASSWORD"`
	ConfigDir              string `env:"CONFIG_DIR"`
	RegistryDataDir        string `env:"REGISTRY_DATA_DIR"`
	ShutdownTimeout        string `env:"SHUTDOWN_TIMEOUT"          envDefault:"30s"`
	NoRegistryFallback     bool   `env:"NO_REGISTRY_FALLBACK"      envDefault:"false"`
	HarborRegistryURL      string `env:"HARBOR_REGISTRY_URL"`
	DirectDelivery         bool   `env:"DIRECT_DELIVERY"           envDefault:"false"`
	ImageDir               string `env:"IMAGE_DIR"`
	// RegistryListen is this satellite's replica-proxy bind (empty disables it).
	RegistryListen string `env:"REGISTRY_LISTEN"`
	// PeerListen is a deprecated alias of RegistryListen.
	PeerListen string `env:"PEER_LISTEN"`
	// PeerURLs is a comma-separated allow-list of replica-proxy URLs for
	// satellites in the same Ground Control group. Out-of-group URLs must
	// not be listed. Empty means replicate from Harbor only.
	PeerURLs string `env:"PEER_URLS"`
}

func (h HarborSatellite) ApplyDefaults() HarborSatellite {
	if h.SPIFFEEndpointSocket == "" {
		h.SPIFFEEndpointSocket = config.DefaultSPIFFEEndpointSocket
	}
	if h.ShutdownTimeout == "" {
		h.ShutdownTimeout = "30s"
	}
	if h.RegistryListen == "" {
		h.RegistryListen = h.PeerListen
	}
	return h
}
