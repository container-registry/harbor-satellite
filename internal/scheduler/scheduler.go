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
	name      SchedulerKey
	cron      *cron.Cron
	processes map[string]Process
	locks     map[string]*sync.Mutex
	stopped   bool
	counter   uint64
	mu        sync.Mutex
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
	s.processes[process.GetName()] = process
	// Add the process to the scheduler
	_, err := s.cron.AddFunc(process.GetCronExpr(), func() {
		s.executeProcess(process)
	})
	if err != nil {
		return fmt.Errorf("error adding process to scheduler: %w", err)
	}
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
