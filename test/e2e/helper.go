package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dagger.io/dagger"
	"github.com/stretchr/testify/require"
)

func getProjectRootDir() (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	return filepath.Abs(filepath.Join(currentDir, "../.."))
}

func generateUniqueSatelliteName(name string) string {
	return fmt.Sprintf("%s-%d", name, time.Now().UnixMicro())
}

func requireNoExecError(t *testing.T, err error, step string) {
	var e *dagger.ExecError
	if errors.As(err, &e) {
		require.NoError(t, err, "failed to "+step+" (exec error)")
	} else {
		require.NoError(t, err, "failed to "+step+" (unexpected error)")
	}
}

func curlContainer(ctx context.Context, c *dagger.Client, cmd []string) (string, error) {
	return c.Container().
		From("curlimages/curl@sha256:9a1ed35addb45476afa911696297f8e115993df459278ed036182dd2cd22b67b").
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithExec(cmd).
		Stdout(ctx)
}
