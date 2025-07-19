package testconfig

import (
	"flag"
	"fmt"
	"os"
)

const (
	AppDir                  = "/app"
	AppBinary               = "app"
	SourceFile              = "cmd/main.go"
	Relative_path           = "./testdata/config.json"
	Absolute_path           = "test/e2e/testdata/config.json"
	Satellite_ping_endpoint = "/api/v1/satellite/ping"
	EnvHarborUsername       = "HARBOR_USERNAME"
	EnvHarborPassword       = "HARBOR_PASSWORD"
)

var ABS bool

func init() {
	flag.BoolVar(&ABS, "abs", true, "Use absolute path for the config file")
}

type Config struct {
	HarborUsername string
	HarborPassword string
}

func Load() (*Config, error) {
	harborUsername := os.Getenv(EnvHarborUsername)
	harborPassword := os.Getenv(EnvHarborPassword)

	if harborPassword == "" || harborUsername == "" {
		return nil, fmt.Errorf("missing required env vars: %s or %s", EnvHarborUsername, EnvHarborPassword)
	}

	return &Config{
		HarborUsername: harborUsername,
		HarborPassword: harborPassword,
	}, nil
}
