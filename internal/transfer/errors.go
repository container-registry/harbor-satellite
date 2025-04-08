package transfer

import "errors"

var (
	// ErrQuotaExceeded is returned when the transfer quota is exceeded
	ErrQuotaExceeded = errors.New("transfer quota exceeded")

	// ErrTransferInProgress is returned when attempting to start a transfer while one is already in progress
	ErrTransferInProgress = errors.New("transfer already in progress")

	// ErrNoTransferInProgress is returned when attempting to end a transfer while none is in progress
	ErrNoTransferInProgress = errors.New("no transfer in progress")
)
