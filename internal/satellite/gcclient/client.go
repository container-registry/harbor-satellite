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
}

// ZTRResult is what registration hands the satellite: its state artifact
// and the robot account that reads it.
type ZTRResult struct {
	StateURL string
	Robot    RobotAccount
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
