package harbor

import (
	"fmt"
	"os"
	"sync"

	"github.com/goharbor/go-client/pkg/harbor"
	v2client "github.com/goharbor/go-client/pkg/sdk/v2.0/client"
)

var (
	clientInstance *v2client.HarborAPI
	clientOnce     sync.Once
	clientErr      error
)

// Returns Harbor v2 client
func GetClient() (*v2client.HarborAPI, error) {
	clientOnce.Do(func() {
		clientConfig := &harbor.ClientSetConfig{
			URL:      os.Getenv("HARBOR_URL"),
			Username: os.Getenv("HARBOR_USERNAME"),
			Password: os.Getenv("HARBOR_PASSWORD"),
		}
		clientInstance = GetClientByConfig(clientConfig)
		if clientErr != nil {
			fmt.Errorf("failed to initialize client: %v", clientErr)
		}
	})
	return clientInstance, clientErr
}

func GetClientByConfig(clientConfig *harbor.ClientSetConfig) *v2client.HarborAPI {
	cs, err := harbor.NewClientSet(clientConfig)
	if err != nil {
		panic(err)
	}
	return cs.V2()
}
