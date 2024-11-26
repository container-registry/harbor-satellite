package scheduler

import "context"

// Process represents a process that can be scheduled
type Process interface {
	// Execute runs the process
	Execute(ctx context.Context) error

	// GetID returns the unique GetID of the process
	GetID() uint64

	// GetName returns the name of the process
	GetName() string

	// GetCronExpr returns the cron expression for the process
	GetCronExpr() string

	// IsRunning returns true if the process is running
	IsRunning() bool
}
