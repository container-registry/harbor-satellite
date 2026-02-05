package state

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

type StatusReportParams struct {
	Name                string    `json:"name"`
	Activity            string    `json:"activity"`
	StateReportInterval string    `json:"state_report_interval"`
	LatestStateDigest   string    `json:"latest_state_digest"`
	LatestConfigDigest  string    `json:"latest_config_digest"`
	MemoryUsedBytes     uint64    `json:"memory_used_bytes"`
	StorageUsedBytes    uint64    `json:"storage_used_bytes"`
	CPUPercent          float64   `json:"cpu_percent"`
	RequestCreatedTime  time.Time `json:"request_created_time"`
	LastSyncDurationMs  int64     `json:"last_sync_duration_ms"`
	ImageCount          int       `json:"image_count"`
}

func collectStatusReportParams(ctx context.Context, heartbeatInterval time.Duration, req *StatusReportParams, cfg config.MetricsConfig) {
	if cfg.CollectCPU {
		req.CPUPercent = getAvgCPUUsage(ctx, 500*time.Millisecond, heartbeatInterval)
	}
	if cfg.CollectMemory {
		req.MemoryUsedBytes = getMemoryUsedBytes(ctx)
	}
	if cfg.CollectStorage {
		req.StorageUsedBytes = getStorageUsedBytes(ctx, "/")
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
	for i := 0; i < samples; i++ {
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
// Supports: "hostname/satellite/satellite-state/<name>/state:latest"
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
		return 0, fmt.Errorf("empty expression provided")
	}
	if !strings.HasPrefix(expr, prefix) {
		return 0, fmt.Errorf("unsupported format: must start with %q", prefix)
	}
	return time.ParseDuration(strings.TrimPrefix(expr, prefix))
}
