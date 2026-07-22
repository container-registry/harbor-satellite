package common

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
)

func (r *Runtime) Initialize() error {
	serverURL, err := url.ParseRequestURI(r.config.GetString(urlKey))
	if err != nil {
		return fmt.Errorf("invalid --server: %w", err)
	}
	if serverURL.Scheme != "http" && serverURL.Scheme != "https" {
		return fmt.Errorf("invalid --server scheme %q: expected http or https", serverURL.Scheme)
	}
	if serverURL.Host == "" {
		return fmt.Errorf("invalid --server: host is required")
	}
	if r.config.GetDuration(timeoutKey) <= 0 {
		return fmt.Errorf("invalid --timeout: must be greater than zero")
	}
	if r.config.GetBool(insecureKey) && serverURL.Scheme != "https" {
		return fmt.Errorf("--insecure can only be used with an HTTPS server")
	}

	httpClient := &http.Client{Timeout: r.config.GetDuration(timeoutKey)}
	if r.config.GetBool(insecureKey) {
		httpClient.Transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{
				MinVersion:         tls.VersionTLS12,
				InsecureSkipVerify: true, //nolint:gosec // Explicitly requested by the CLI user.
			},
		}
	}

	doer := &storedTokenDoer{client: httpClient, runtime: r}
	options := []groundcontrol.ClientOption{groundcontrol.WithHTTPClient(doer)}
	if token := strings.TrimSpace(r.config.GetString(tokenKey)); token != "" {
		options = append(options, groundcontrol.WithRequestEditorFn(
			func(_ context.Context, request *http.Request) error {
				request.Header.Set("Authorization", "Bearer "+token)
				return nil
			},
		))
	}

	r.client, err = groundcontrol.NewClientWithResponses(r.config.GetString(urlKey), options...)
	if err != nil {
		return fmt.Errorf("create Ground Control client: %w", err)
	}
	return nil
}

func (r *Runtime) Client() *groundcontrol.ClientWithResponses {
	return r.client
}

func (r *Runtime) ValidateAuth() error {
	if strings.TrimSpace(r.config.GetString(tokenKey)) == "" {
		return fmt.Errorf("authentication token is required: run auth login, use --token, or set GROUND_CONTROL_TOKEN")
	}
	return nil
}

type storedTokenDoer struct {
	client  *http.Client
	runtime *Runtime
}

func (d *storedTokenDoer) Do(request *http.Request) (*http.Response, error) {
	// The generated client constructs this request from the server URL validated in Initialize.
	response, err := d.client.Do(request) //nolint:gosec // Request destination has already been validated.
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusUnauthorized || !d.runtime.UsingStoredToken() {
		return response, nil
	}
	if err := d.runtime.RemoveStoredToken(); err != nil {
		_ = response.Body.Close()
		return nil, fmt.Errorf("remove invalid stored token: %w", err)
	}
	return response, nil
}
