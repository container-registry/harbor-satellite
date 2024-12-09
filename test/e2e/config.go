package e2e

import "flag"

const (
	appDir                  = "/app"
	appBinary               = "app"
	sourceFile              = "main.go"
	relative_path           = "./testdata/config.json"
	absolute_path           = "test/e2e/testdata/config.json"
	satellite_ping_endpoint = "/api/v1/satellite/ping"
)

var ABS bool

func init() {
	flag.BoolVar(&ABS, "abs", true, "Use absolute path for the config file")
}
