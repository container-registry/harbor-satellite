package transfer

import (
	"errors"
	"sync"
	"time"
)

// TransferMeter tracks data transfer usage and enforces rate limits
type TransferMeter struct {
	mu sync.RWMutex
	// Current transfer stats
	currentHour  int64
	currentDay   int64
	currentWeek  int64
	currentMonth int64
	// Rate limits
	hourlyLimit  int64
	dailyLimit   int64
	weeklyLimit  int64
	monthlyLimit int64
	// Last reset times
	lastHourReset  time.Time
	lastDayReset   time.Time
	lastWeekReset  time.Time
	lastMonthReset time.Time

	// For immediate transfer tracking
	currentTransferBytes int64
	lastTransferTime     time.Time
	isTransferring       bool
}

// NewTransferMeter creates a new transfer meter with specified limits
func NewTransferMeter(hourly, daily, weekly, monthly int64) *TransferMeter {
	now := time.Now()
	return &TransferMeter{
		hourlyLimit:      hourly,
		dailyLimit:       daily,
		weeklyLimit:      weekly,
		monthlyLimit:     monthly,
		lastHourReset:    now,
		lastDayReset:     now,
		lastWeekReset:    now,
		lastMonthReset:   now,
		lastTransferTime: now,
	}
}

// RecordTransfer records a data transfer of the specified size
func (tm *TransferMeter) RecordTransfer(size int64) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	now := time.Now()

	tm.resetIfNeeded(now)

	if tm.hourlyLimit > 0 && tm.currentHour+size > tm.hourlyLimit {
		return ErrHourlyLimitExceeded
	}
	if tm.dailyLimit > 0 && tm.currentDay+size > tm.dailyLimit {
		return ErrDailyLimitExceeded
	}
	if tm.weeklyLimit > 0 && tm.currentWeek+size > tm.weeklyLimit {
		return ErrWeeklyLimitExceeded
	}
	if tm.monthlyLimit > 0 && tm.currentMonth+size > tm.monthlyLimit {
		return ErrMonthlyLimitExceeded
	}

	tm.currentHour += size
	tm.currentDay += size
	tm.currentWeek += size
	tm.currentMonth += size

	tm.currentTransferBytes = size

	return nil
}

// GetUsage returns current transfer usage for all time periods
func (tm *TransferMeter) GetUsage() TransferStats {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	now := time.Now()
	tm.resetIfNeeded(now)

	return TransferStats{
		Hourly:  tm.currentHour,
		Daily:   tm.currentDay,
		Weekly:  tm.currentWeek,
		Monthly: tm.currentMonth,
	}
}

// resetIfNeeded resets counters if their time periods have elapsed
func (tm *TransferMeter) resetIfNeeded(now time.Time) {
	if now.Sub(tm.lastHourReset) >= time.Hour {
		tm.currentHour = 0
		tm.lastHourReset = now
	}
	if now.Sub(tm.lastDayReset) >= 24*time.Hour {
		tm.currentDay = 0
		tm.lastDayReset = now
	}
	if now.Sub(tm.lastWeekReset) >= 7*24*time.Hour {
		tm.currentWeek = 0
		tm.lastWeekReset = now
	}
	if now.Month() != tm.lastMonthReset.Month() || now.Year() != tm.lastMonthReset.Year() {
		tm.currentMonth = 0
		tm.lastMonthReset = now
	}
}

// CheckQuota checks if the current transfer is within quota
func (tm *TransferMeter) CheckQuota() bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.isTransferring {
		return true
	}

	return tm.currentHour < tm.hourlyLimit
}

// StartTransfer starts a new transfer
func (tm *TransferMeter) StartTransfer() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.isTransferring = true
	tm.currentTransferBytes = 0
	tm.lastTransferTime = time.Now()
}

// EndTransfer ends the current transfer and records the bytes transferred
func (tm *TransferMeter) EndTransfer(bytes int64) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.isTransferring = false
	tm.currentTransferBytes = bytes

	tm.RecordTransfer(bytes)
}

// GetCurrentTransferBytes returns the current transfer bytes
func (tm *TransferMeter) GetCurrentTransferBytes() int64 {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.currentTransferBytes
}

// UpdateTransferBytes updates the current transfer bytes
func (tm *TransferMeter) UpdateTransferBytes(bytes int64) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.currentTransferBytes = bytes
}

// TransferStats holds current transfer usage statistics
type TransferStats struct {
	Hourly  int64
	Daily   int64
	Weekly  int64
	Monthly int64
}

// Errors
var (
	ErrHourlyLimitExceeded  = errors.New("hourly transfer limit exceeded")
	ErrDailyLimitExceeded   = errors.New("daily transfer limit exceeded")
	ErrWeeklyLimitExceeded  = errors.New("weekly transfer limit exceeded")
	ErrMonthlyLimitExceeded = errors.New("monthly transfer limit exceeded")
)
