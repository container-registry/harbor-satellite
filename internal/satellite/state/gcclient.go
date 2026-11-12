package state

import (
	"math"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/satellite/gcclient"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
)

func newGCClient(cm *config.ConfigManager, httpClient *http.Client, creds gcclient.Credentials) (*gcclient.Client, error) {
	return gcclient.New(gcclient.Options{
		BaseURL:       cm.ResolveGroundControlURL(),
		HTTPClient:    httpClient,
		Credentials:   creds,
		AllowInsecure: cm.UseUnsecure(),
	})
}

// robotCredentials authenticates as the Harbor robot, read per request so a
// refreshed secret applies at once. Without a robot it sends nothing.
func robotCredentials(cm *config.ConfigManager) gcclient.Credentials {
	if cm.GetSourceRegistryUsername() == "" {
		return gcclient.NoCredentials{}
	}
	return gcclient.RobotBasic(func() (string, string) {
		return cm.GetSourceRegistryUsername(), cm.GetSourceRegistryPassword()
	})
}

// gcCredentials prefer the Ground Control session (ADR-0016) and fall back
// to the robot for a satellite onboarded before sessions; both are read per
// request, so a renewal applies at once.
func gcCredentials(cm *config.ConfigManager) gcclient.Credentials {
	if cm.GetGCSession().Token == "" {
		return robotCredentials(cm)
	}
	return gcclient.SessionBearer(func() string { return cm.GetGCSession().Token })
}

func stateConfigFromZTR(res gcclient.ZTRResult) config.StateConfig {
	sc := config.StateConfig{
		StateURL: res.StateURL,
		RegistryCredentials: config.RegistryCredentials{
			URL:      config.URL(res.Robot.RegistryURL),
			Username: res.Robot.Username,
			Password: res.Robot.Password,
		},
	}
	if res.Session != nil {
		sc.GCSession = config.GCSession{Token: res.Session.Token, ExpiresAt: res.Session.ExpiresAt}
	}
	return sc
}

func toStatusReport(p *StatusReportParams) gcclient.StatusReport {
	report := gcclient.StatusReport{
		Name:                p.Name,
		Activity:            p.Activity,
		StateReportInterval: p.StateReportInterval,
		LatestStateDigest:   p.LatestStateDigest,
		LatestConfigDigest:  p.LatestConfigDigest,
		MemoryUsedBytes:     clampInt64(p.MemoryUsedBytes),
		StorageUsedBytes:    clampInt64(p.StorageUsedBytes),
		CPUPercent:          p.CPUPercent,
		RequestCreatedTime:  p.RequestCreatedTime,
		LastSyncDurationMs:  p.LastSyncDurationMs,
		ImageCount:          clampInt32(p.ImageCount),
	}
	for _, a := range p.AppliedStates {
		report.AppliedStates = append(report.AppliedStates, groundcontrol.AppliedState{Ref: a.Ref, Digest: a.Digest})
	}
	report.LastReplicationError = p.LastReplicationError
	for _, img := range p.CachedImages {
		report.CachedImages = append(report.CachedImages, gcclient.CachedImageReport{Reference: img.Reference, SizeBytes: img.SizeBytes})
	}
	return report
}

func clampInt64(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

func clampInt32(v int) int32 {
	switch {
	case v > math.MaxInt32:
		return math.MaxInt32
	case v < math.MinInt32:
		return math.MinInt32
	default:
		return int32(v)
	}
}
