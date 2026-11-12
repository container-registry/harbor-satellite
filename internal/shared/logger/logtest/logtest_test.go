package logtest_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/container-registry/harbor-satellite/internal/shared/logger/logtest"
	"github.com/stretchr/testify/require"
)

func TestCapture_CollectsContextLoggerOutput(t *testing.T) {
	ctx, log, buf := logtest.Capture(t)

	logger.FromContext(ctx).Debug().Str("from", "context").Msg("first")
	log.Info().Msg("second")

	out := buf.String()
	require.Contains(t, out, `"from":"context"`)
	require.Contains(t, out, `"message":"first"`)
	require.Contains(t, out, `"message":"second"`)
}

func TestBuffer_ConcurrentWriters(t *testing.T) {
	_, log, buf := logtest.Capture(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				log.Info().Msg("line")
			}
		})
	}
	wg.Wait()

	require.Equal(t, 800, strings.Count(buf.String(), "\n"))
}
