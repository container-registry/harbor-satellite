package state

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

type StatusReportParams struct {
	Name                string    `json:"name"`                  // Satellite identifier
	Activity            string    `json:"activity"`              // Current activity satellite is doing
	StateReportInterval string    `json:"state_report_interval"` // Interval between status reports
	LatestStateDigest   string    `json:"latest_state_digest"`   // Digest of latest state artifact
	LatestConfigDigest  string    `json:"latest_config_digest"`  // Digest of latest config artifact
	MemoryUsedBytes     uint64    `json:"memory_used_bytes"`     // Memory currently used by satellite
	StorageUsedBytes    uint64    `json:"storage_used_bytes"`    // Storage currently used by satellite
	CPUPercent          float64   `json:"cpu_percent"`           // CPU usage percentage
	RequestCreatedTime  time.Time `json:"request_created_time"`  // Timestamp of request creation
}

func collectStatusReportParams(ctx context.Context, duration time.Duration, req *StatusReportParams) error {
	cpuPercent, err := getAvgCpuUsage(ctx, 1*time.Second, duration)
	if err != nil {
		return err
	}

	memUsed, err := getMemoryUsedBytes(ctx)
	if err != nil {
		return err
	}

	storUsed, err := getStorageUsedBytes(ctx, "/")
	if err != nil {
		return err
	}

	req.CPUPercent = cpuPercent
	req.MemoryUsedBytes = memUsed
	req.StorageUsedBytes = storUsed
	req.RequestCreatedTime = time.Now()

	return nil
}

func getAvgCpuUsage(ctx context.Context, sampleInterval time.Duration, totalDuration time.Duration) (float64, error) {
	var sum float64
	var count int

	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()

	timeout := time.After(totalDuration)

	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-timeout:
			if count == 0 {
				return 0, fmt.Errorf("no samples collected")
			}
			avg := sum / float64(count)
			return math.Round(avg*100) / 100, nil
		case <-ticker.C:
			percent, err := cpu.PercentWithContext(ctx, 0, false)
			if err != nil {
				continue
			}
			if len(percent) > 0 {
				sum += percent[0]
				count++
			}
		}
	}
}

func getStorageUsedBytes(ctx context.Context, path string) (uint64, error) {
	usageStat, err := disk.UsageWithContext(ctx, path)
	if err != nil {
		return 0, fmt.Errorf("failed to get storage used: %w", err)
	}
	return usageStat.Used, nil
}

func getMemoryUsedBytes(ctx context.Context) (uint64, error) {
	vmStat, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get memory used: %w", err)
	}
	return vmStat.Used, nil
}

func extractSatelliteNameFromURL(stateURL string) (string, error) {
	u, err := url.Parse(stateURL)
	if err != nil {
		return "", fmt.Errorf("invalid state URL %q: %w", stateURL, err)
	}

	parts := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	if len(parts) < 4 {
		return "", fmt.Errorf("state URL %q does not have enough path segments to extract satellite name", stateURL)
	}

	return parts[3], nil
}
