package common

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/spf13/viper"
)

const (
	URLKey      = "url"
	TokenKey    = "token"
	TimeoutKey  = "timeout"
	InsecureKey = "insecure"
)

// Runtime owns the configuration used by all CLI commands.
type Runtime struct {
	config *viper.Viper
	client *groundcontrol.ClientWithResponses
}

func NewRuntime(config *viper.Viper) *Runtime {
	return &Runtime{config: config}
}

func (r *Runtime) Initialize() error {
	serverURL, err := url.ParseRequestURI(r.config.GetString(URLKey))
	if err != nil {
		return fmt.Errorf("invalid --server: %w", err)
	}
	if serverURL.Scheme != "http" && serverURL.Scheme != "https" {
		return fmt.Errorf("invalid --server scheme %q: expected http or https", serverURL.Scheme)
	}
	if serverURL.Host == "" {
		return fmt.Errorf("invalid --server: host is required")
	}
	if r.config.GetDuration(TimeoutKey) <= 0 {
		return fmt.Errorf("invalid --timeout: must be greater than zero")
	}
	if r.config.GetBool(InsecureKey) && serverURL.Scheme != "https" {
		return fmt.Errorf("--insecure can only be used with an HTTPS server")
	}

	httpClient := &http.Client{Timeout: r.config.GetDuration(TimeoutKey)}
	if r.config.GetBool(InsecureKey) {
		httpClient.Transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{
				MinVersion:         tls.VersionTLS12,
				InsecureSkipVerify: true, //nolint:gosec // Explicitly requested by the CLI user.
			},
		}
	}

	options := []groundcontrol.ClientOption{groundcontrol.WithHTTPClient(httpClient)}
	if token := strings.TrimSpace(r.config.GetString(TokenKey)); token != "" {
		options = append(options, groundcontrol.WithRequestEditorFn(
			func(_ context.Context, request *http.Request) error {
				request.Header.Set("Authorization", "Bearer "+token)
				return nil
			},
		))
	}

	r.client, err = groundcontrol.NewClientWithResponses(r.config.GetString(URLKey), options...)
	if err != nil {
		return fmt.Errorf("create Ground Control client: %w", err)
	}
	return nil
}

func (r *Runtime) Client() *groundcontrol.ClientWithResponses {
	return r.client
}

func (r *Runtime) ValidateAuth() error {
	if strings.TrimSpace(r.config.GetString(TokenKey)) == "" {
		return fmt.Errorf("authentication token is required: use --token or GROUND_CONTROL_TOKEN")
	}
	return nil
}
