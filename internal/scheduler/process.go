package scheduler

import (
	"context"
)

// Process represents a process that can be scheduled
type Process interface {
	// Name returns the name of the process
	Name() string

	// Execute runs the process
	Execute(ctx context.Context, upstreamPayload *UpstreamInfo) error

	// IsRunning returns true if the process is running
	IsRunning() bool

	// ShouldStop returns true if the process scheduling should be stopped
	IsComplete() bool
}
