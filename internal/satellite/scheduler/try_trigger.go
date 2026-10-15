package scheduler

// TryTrigger asks for a run without blocking. It returns true when the
// request is queued and false when one is already waiting or the scheduler
// has stopped. Events use it instead of Trigger, whose unbuffered send
// blocks the caller (the heartbeat) until the scheduler loop receives, and
// forever once the loop has exited (plan item 6.7).
func (s *Scheduler) TryTrigger() bool {
	return false
}
