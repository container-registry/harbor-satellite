package state

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

func collectStatusReportParams(
	ctx context.Context,
	heartbeatInterval time.Duration,
	req *groundcontrol.SatelliteStatusRequest,
	cfg config.MetricsConfig,
	registryURL string,
	insecure bool,
) {
	log := logger.FromContext(ctx)

	if cfg.CollectCPU {
		req.CPUPercent = getAvgCPUUsage(ctx, 500*time.Millisecond, heartbeatInterval)
	}
	if cfg.CollectMemory {
		req.MemoryUsedBytes = getMemoryUsedBytes(ctx)
	}
	if cfg.CollectStorage {
		req.StorageUsedBytes = getStorageUsedBytes(ctx, "/")
	}

	if registryURL != "" {
		cached, err := collectCachedImages(ctx, registryURL, insecure)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to collect cached images")
		} else {
			req.CachedImages = cached
			req.ImageCount = int32(len(cached))
		}
	}
}

func getAvgCPUUsage(ctx context.Context, sampleInterval, totalDuration time.Duration) float64 {
	if totalDuration <= 0 || sampleInterval <= 0 {
		return 0
	}
	samples := int(totalDuration / sampleInterval)
	if samples < 1 {
		samples = 1
	}

	var total float64
	var count int
	for range samples {
		if ctx.Err() != nil {
			break
		}
		percents, err := cpu.PercentWithContext(ctx, sampleInterval, false)
		if err != nil || len(percents) == 0 {
			continue
		}
		total += percents[0]
		count++
	}

	if count == 0 {
		return 0
	}

	return total / float64(count)
}

func getMemoryUsedBytes(ctx context.Context) uint64 {
	v, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return 0
	}

	return v.Used
}

func getStorageUsedBytes(ctx context.Context, path string) uint64 {
	usage, err := disk.UsageWithContext(ctx, path)
	if err != nil {
		return 0
	}

	return usage.Used
}

// extractSatelliteNameFromURL parses a state URL and returns the satellite name.
// Supports: "hostname/satellite/satellite-state/<name>/state:latest".
func extractSatelliteNameFromURL(stateURL string) (string, error) {
	parsed, err := url.Parse(stateURL)
	if err != nil {
		return "", fmt.Errorf("parse state URL: %w", err)
	}

	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	// Path format: /satellite/satellite-state/<name>/state:latest
	for i, part := range parts {
		if part == "satellite-state" && i+1 < len(parts) {
			return parts[i+1], nil
		}
	}

	return "", fmt.Errorf("could not extract satellite name from URL path: %s", parsed.Path)
}

func parseEveryExpr(expr string) (time.Duration, error) {
	const prefix = "@every "
	if expr == "" {
		return 0, errors.New("empty expression provided")
	}
	if !strings.HasPrefix(expr, prefix) {
		return 0, fmt.Errorf("unsupported format: must start with %q", prefix)
	}

	return time.ParseDuration(strings.TrimPrefix(expr, prefix))
}
