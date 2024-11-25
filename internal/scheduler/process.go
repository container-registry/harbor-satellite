package scheduler

import (
	"context"

	"github.com/robfig/cron/v3"
)

// Process represents a process that can be scheduled
type Process interface {
	// Execute runs the process
	Execute(ctx context.Context) error

	// GetID returns the unique GetID of the process
	GetID() cron.EntryID

	// SetID sets the unique GetID of the process
	SetID(id cron.EntryID)

	// GetName returns the name of the process
	GetName() string

	// GetCronExpr returns the cron expression for the process
	GetCronExpr() string

	// IsRunning returns true if the process is running
	IsRunning() bool

	// CanExecute returns true if all conditions are fulfilled to execute the process
	CanExecute(ctx context.Context) (bool, string)

	// AddEventBroker adds the event broker to the process here the process could subscribe to the events
	AddEventBroker(eventBroker *EventBroker, ctx context.Context)
}
