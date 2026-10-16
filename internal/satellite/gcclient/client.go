// Package gcclient is the satellite's only Ground Control client. Every
// request goes through the generated client in pkg/groundcontrol, so paths
// come from the spec instead of string joins. It returns its own result
// types; the state package maps them into pkg/config.
package gcclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
)

type Options struct {
	// BaseURL is the ground_control_url; a trailing slash and a path prefix
	// are allowed.
	BaseURL string
	// HTTPClient carries the TLS or SPIFFE mutual TLS setup built by the
	// caller.
	HTTPClient  *http.Client
	Credentials Credentials
	// AllowInsecure is use_unsecure. Without it BaseURL must be https.
	AllowInsecure bool
}

type StatusReport = groundcontrol.SatelliteStatusRequest

type SyncResult struct {
	Events []string
	// Journal answers the report's journal; nil when Ground Control
	// ignores journals (plan item 6.5).
	Journal *JournalAck
	// Session is a new or renewed Ground Control session; nil when the
	// current one stays (plan item 4.1).
	Session *Session
	// InventoryRequested asks for cached_images with the next report,
	// because Ground Control has no inventory for its digest (plan item 4.4).
	InventoryRequested bool
	// Credentials is a successor robot to switch to; nil when there is no
	// rotation for the satellite (plan item 8.4).
	Credentials *RotatedCredentials
}

// RotatedCredentials are a successor robot's credentials. The satellite
// switches to them and reports Generation with its next heartbeat.
type RotatedCredentials struct {
	Username, Password string
	ExpiresAt          time.Time
	Generation         int64
}

// JournalAck is Ground Control's answer to a journal upload: its epoch for
// the satellite, the seq it holds that epoch up to, and why it rejected
// the upload, if it did.
type JournalAck struct {
	Epoch, AckedSeq int64
	Error           string
}

// ZTRResult is what registration hands the satellite: its state artifact
// and the robot account that reads it.
type ZTRResult struct {
	StateURL string
	Robot    RobotAccount
	// Session is nil for SPIFFE ZTR, whose SVID authenticates every beat.
	Session *Session
}

type RobotAccount struct {
	RegistryURL string
	Username    string
	Password    string
}

type RefreshResult struct {
	Secret string
}

var (
	ErrUnauthorized = errors.New("ground control rejected the satellite credentials")
	ErrForbidden    = errors.New("ground control refused the satellite")
	ErrRateLimited  = errors.New("ground control rate limit exceeded")
)

// StatusError is a Ground Control response with a status no sentinel covers.
type StatusError struct {
	StatusCode int
	Message    string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("ground control returned %d: %s", e.StatusCode, e.Message)
}

type Client struct{}

func New(opts Options) (*Client, error) {
	return nil, fmt.Errorf("%w: D1 Ground Control client for %s", notimpl.ErrNotImplemented, opts.BaseURL)
}

// ZeroTouch registers the satellite with its ZTR token.
func (c *Client) ZeroTouch(ctx context.Context, token string) (ZTRResult, error) {
	return ZTRResult{}, notimpl.Err("D1 token ZTR via generated client")
}

// SPIFFEZeroTouch registers the satellite with its SVID.
func (c *Client) SPIFFEZeroTouch(ctx context.Context) (ZTRResult, error) {
	return ZTRResult{}, notimpl.Err("D1 SPIFFE ZTR via generated client")
}

// Sync sends the heartbeat and returns the events Ground Control queued for
// the satellite.
func (c *Client) Sync(ctx context.Context, report StatusReport) (SyncResult, error) {
	return SyncResult{}, notimpl.Err("D1 heartbeat via generated client")
}

// RefreshCredentials asks Ground Control to rotate the satellite's robot
// secret and returns the new one.
func (c *Client) RefreshCredentials(ctx context.Context) (RefreshResult, error) {
	return RefreshResult{}, notimpl.Err("D2 credential refresh via generated client")
}
