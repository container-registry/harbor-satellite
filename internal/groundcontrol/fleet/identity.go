package fleet

import (
	"errors"
	"time"
)

// DeviceAuthorization is the start of an OAuth 2.0 device flow (RFC 8628)
// for operators who log in from the CLI (plan item 8.1).
type DeviceAuthorization struct {
	DeviceCode, UserCode, VerificationURI, VerificationURIComplete string
	ExpiresIn, Interval                                            time.Duration
}

// The device flow's poll answers, as RFC 8628 names them.
var (
	ErrAuthorizationPending = errors.New("authorization_pending")
	ErrSlowDown             = errors.New("slow_down")
)
