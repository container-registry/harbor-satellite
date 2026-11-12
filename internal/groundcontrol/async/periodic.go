package async

import (
	"context"
	"time"
)

// every runs fn at once and then every interval until ctx ends. An error
// of fn is the job's to log; the loop keeps going so one failed run never
// stops a housekeeping job.
func every(ctx context.Context, interval time.Duration, fn func(context.Context)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		fn(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
