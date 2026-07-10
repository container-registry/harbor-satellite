package harbor

import (
	"sync"

	"github.com/container-registry/harbor-satellite/internal/env"
	"github.com/goharbor/go-client/pkg/harbor"
	v2client "github.com/goharbor/go-client/pkg/sdk/v2.0/client"
)

var (
	client     *v2client.HarborAPI
	clientOnce sync.Once
)

// Returns Harbor v2 client
func GetClient() *v2client.HarborAPI {
	clientOnce.Do(func() {
		cfg := env.GC.Harbor
		clientConfig := &harbor.ClientSetConfig{
			URL:      cfg.URL,
			Username: cfg.Username,
			Password: cfg.Password,
		}
		client = GetClientByConfig(clientConfig)
	})

	return client
}

func GetClientByConfig(clientConfig *harbor.ClientSetConfig) *v2client.HarborAPI {
	cs, err := harbor.NewClientSet(clientConfig)
	if err != nil {
		panic(err)
	}
	return cs.V2()
}
