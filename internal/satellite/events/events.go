package events

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	satTLS "github.com/container-registry/harbor-satellite/internal/satellite/tls"
	"github.com/container-registry/harbor-satellite/pkg/config"
)

func createHTTPClient(tlsCfg config.TLSConfig, useUnsecure bool) (*http.Client, error) {
	transport := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: true,
	}

	if useUnsecure {
		transport.TLSClientConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		transport.TLSClientConfig.InsecureSkipVerify = useUnsecure
	} else if tlsCfg.CertFile != "" || tlsCfg.CAFile != "" {
		cfg := &satTLS.Config{
			CertFile:   tlsCfg.CertFile,
			KeyFile:    tlsCfg.KeyFile,
			CAFile:     tlsCfg.CAFile,
			SkipVerify: tlsCfg.SkipVerify,
			MinVersion: tls.VersionTLS12,
		}

		tlsConfig, err := satTLS.LoadClientTLSConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("load TLS config: %w", err)
		}
		transport.TLSClientConfig = tlsConfig
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}, nil
}
