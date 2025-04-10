package harbor

import (
	"os"
	"sync"

	"github.com/goharbor/go-client/pkg/harbor"
	v2client "github.com/goharbor/go-client/pkg/sdk/v2.0/client"
	_ "github.com/joho/godotenv/autoload"
)

var (
	client     *v2client.HarborAPI
	clientOnce sync.Once
)

// Returns Harbor v2 client
func GetClient() *v2client.HarborAPI {
	clientOnce.Do(func() {
		clientConfig := &harbor.ClientSetConfig{
			URL:      os.Getenv("HARBOR_URL"),
			Username: os.Getenv("HARBOR_USERNAME"),
			Password: os.Getenv("HARBOR_PASSWORD"),
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
