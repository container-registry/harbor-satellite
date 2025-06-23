package e2e

import (
	"log"
	"os"
	"testing"

	"github.com/container-registry/harbor-satellite/test/e2e/testconfig"
)

var cfg *testconfig.Config

func TestMain(m *testing.M) {
	var err error
	cfg, err = testconfig.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	os.Exit(m.Run())
}
