package harbor

import (
	"sync"

	"github.com/container-registry/harbor-satellite/internal/env"
	"github.com/goharbor/go-client/pkg/harbor"
	v2client "github.com/goharbor/go-client/pkg/sdk/v2.0/client"
)

var (
	client     *v2client.HarborAPI
	clientErr  error
	clientOnce sync.Once
)

// GetClient returns the Harbor v2 client or an error if initialization fails.
func GetClient() (*v2client.HarborAPI, error) {
	clientOnce.Do(func() {
		cfg := env.GC.Harbor
		clientConfig := &harbor.ClientSetConfig{
			URL:      cfg.URL,
			Username: cfg.Username,
			Password: cfg.Password,
		}
		client, clientErr = GetClientByConfig(clientConfig)
	})

	if clientErr != nil {
		return nil, clientErr
	}
	return client, nil
}

// GetClientByConfig initializes and returns the Harbor client set using the configuration.
func GetClientByConfig(clientConfig *harbor.ClientSetConfig) (*v2client.HarborAPI, error) {
	cs, err := harbor.NewClientSet(clientConfig)
	if err != nil {
		return nil, err
	}
	return cs.V2(), nil
}
