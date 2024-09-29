package scheduler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"container-registry.com/harbor-satellite/logger"
	"github.com/robfig/cron/v3"
)

type SchedulerKey string

const BasicSchedulerKey SchedulerKey = "basic-scheduler"

type Scheduler interface {
	// GetSchedulerKey would return the key of the scheduler which is unique and for a particular scheduler and is used to get the scheduler from the context
	GetSchedulerKey() SchedulerKey
	// Schedule would add a process to the scheduler
	Schedule(process Process) error
	// Start would start the scheduler
	Start() error
	// Stop would stop the scheduler
	Stop() error
	// NextID would return the next unique ID
	NextID() uint64
}

type BasicScheduler struct {
	// name is the key of the scheduler
	name      SchedulerKey
	// cron is the cron scheduler
	cron      *cron.Cron
	// processes is a map of processes which are attached to the scheduler
	processes map[string]Process
	// locks is a map of locks for each process which is used to schedule if the process are interdependent
	locks     map[string]*sync.Mutex
	// stopped is a flag to check if the scheduler is stopped
	stopped   bool
	// counter is the counter for the unique ID of the process
	counter   uint64
	// mu is the mutex for the scheduler
	mu        sync.Mutex
	// ctx is the context of the scheduler
	ctx       context.Context
}

func NewBasicScheduler(ctx *context.Context) Scheduler {
	return &BasicScheduler{
		cron:      cron.New(),
		processes: make(map[string]Process),
		locks:     make(map[string]*sync.Mutex),
		mu:        sync.Mutex{},
		name:      BasicSchedulerKey,
		ctx:       *ctx,
	}
}

func (s *BasicScheduler) GetSchedulerKey() SchedulerKey {
	return s.name
}

func (s *BasicScheduler) NextID() uint64 {
	return atomic.AddUint64(&s.counter, 1)
}

func (s *BasicScheduler) Schedule(process Process) error {
	log := logger.FromContext(s.ctx)
	log.Info().Msgf("Scheduling process %s", process.GetName())
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, processes := range s.processes {
		if process.GetName() == processes.GetName() {
			return fmt.Errorf("process with Name %s already exists", process.GetName())
		}
	}
	// Add the process to the scheduler
	_, err := s.cron.AddFunc(process.GetCronExpr(), func() {
		s.executeProcess(process)
	})
	if err != nil {
		return fmt.Errorf("error adding process to scheduler: %w", err)
	}
	s.processes[process.GetName()] = process
	log.Info().Msgf("Process %s scheduled with cron expression %s", process.GetName(), process.GetCronExpr())
	return nil
}

func (s *BasicScheduler) Start() error {
	s.cron.Start()
	return nil
}

func (s *BasicScheduler) Stop() error {
	s.stopped = true
	s.cron.Stop()
	return nil
}

func (s *BasicScheduler) executeProcess(process Process) error {
	if s.stopped {
		return fmt.Errorf("scheduler is stopped")
	}
	// Execute the process
	return process.Execute(s.ctx)
}
