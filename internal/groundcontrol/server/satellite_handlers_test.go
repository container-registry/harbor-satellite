package server

import (
	"errors"
	"os"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/stretchr/testify/require"
)

func loadGCForTest(t *testing.T) {
	t.Helper()

	previous := env.GC
	require.NoError(t, env.LoadGC())
	t.Cleanup(func() {
		env.GC = previous
	})
}

func TestMain(m *testing.M) {
	// Ensure SKIP_HARBOR_HEALTH_CHECK doesn't leak between tests
	origValue := os.Getenv("SKIP_HARBOR_HEALTH_CHECK")
	harborStub = startHarborStub()
	code := m.Run()
	harborStub.Close()
	if origValue != "" {
		_ = os.Setenv("SKIP_HARBOR_HEALTH_CHECK", origValue)
	} else {
		_ = os.Unsetenv("SKIP_HARBOR_HEALTH_CHECK")
	}
	os.Exit(code)
}

// Unused variable to satisfy linter
var _ = errors.New
