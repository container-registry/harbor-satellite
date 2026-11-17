// Package gcclient is the satellite's only Ground Control client. Every
// request goes through the generated client in pkg/groundcontrol, so paths
// come from the spec instead of string joins. It returns its own result
// types; the state package maps them into pkg/config.
package gcclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

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

type Client struct {
	api *groundcontrol.Client
}

const defaultTimeout = 30 * time.Second

// maxResponseBytes bounds what the satellite reads from one Ground Control
// response; a healthy answer is a few kilobytes.
const maxResponseBytes = 4 << 20

func New(opts Options) (*Client, error) {
	u, err := url.Parse(opts.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse ground control URL: %w", err)
	}
	if u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("ground control URL %q must be an absolute http or https URL", opts.BaseURL)
	}
	if u.Scheme != "https" && !opts.AllowInsecure {
		return nil, fmt.Errorf("ground control URL %q must use https unless use_unsecure is set", opts.BaseURL)
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	creds := opts.Credentials
	if creds == nil {
		creds = NoCredentials{}
	}
	api, err := groundcontrol.NewClient(opts.BaseURL,
		groundcontrol.WithHTTPClient(httpClient),
		groundcontrol.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			creds.Authorize(req)
			return nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("create ground control client: %w", err)
	}
	return &Client{api: api}, nil
}

// ZeroTouch registers the satellite with its ZTR token.
func (c *Client) ZeroTouch(ctx context.Context, token string) (ZTRResult, error) {
	var out groundcontrol.StateConfigResponse
	resp, err := c.api.Ztr(ctx, groundcontrol.ZTRRequest{Token: token})
	if err != nil {
		return ZTRResult{}, fmt.Errorf("zero-touch registration: %w", err)
	}
	defer resp.Body.Close()
	if err := decode(resp, &out); err != nil {
		return ZTRResult{}, fmt.Errorf("zero-touch registration: %w", err)
	}
	return ztrResult(out), nil
}

// SPIFFEZeroTouch registers the satellite with its SVID.
func (c *Client) SPIFFEZeroTouch(ctx context.Context) (ZTRResult, error) {
	var out groundcontrol.StateConfigResponse
	resp, err := c.api.SpiffeZtr(ctx)
	if err != nil {
		return ZTRResult{}, fmt.Errorf("SPIFFE zero-touch registration: %w", err)
	}
	defer resp.Body.Close()
	if err := decode(resp, &out); err != nil {
		return ZTRResult{}, fmt.Errorf("SPIFFE zero-touch registration: %w", err)
	}
	return ztrResult(out), nil
}

// Sync sends the heartbeat and returns the events Ground Control queued for
// the satellite.
func (c *Client) Sync(ctx context.Context, report StatusReport) (SyncResult, error) {
	var out groundcontrol.SatelliteSyncResponse
	resp, err := c.api.SyncSatellite(ctx, report)
	if err != nil {
		return SyncResult{}, fmt.Errorf("status report: %w", err)
	}
	defer resp.Body.Close()
	if err := decode(resp, &out); err != nil {
		return SyncResult{}, fmt.Errorf("status report: %w", err)
	}
	res := SyncResult{Events: out.Events, InventoryRequested: out.InventoryRequested}
	if out.Journal != nil {
		res.Journal = &JournalAck{Epoch: out.Journal.Epoch, AckedSeq: out.Journal.AckedSeq, Error: out.Journal.Error}
	}
	res.Session = session(out.Session)
	if out.Credentials != nil {
		res.Credentials = &RotatedCredentials{
			Username:   out.Credentials.Username,
			Password:   out.Credentials.Password,
			ExpiresAt:  out.Credentials.ExpiresAt,
			Generation: out.Credentials.Generation,
		}
	}
	return res, nil
}

// RefreshCredentials asks Ground Control to rotate the satellite's robot
// secret and returns the new one.
func (c *Client) RefreshCredentials(ctx context.Context) (RefreshResult, error) {
	var out groundcontrol.RefreshCredentialsResponse
	resp, err := c.api.RefreshSatelliteCredentials(ctx)
	if err != nil {
		return RefreshResult{}, fmt.Errorf("credential refresh: %w", err)
	}
	defer resp.Body.Close()
	if err := decode(resp, &out); err != nil {
		return RefreshResult{}, fmt.Errorf("credential refresh: %w", err)
	}
	if out.Secret == "" {
		return RefreshResult{}, errors.New("credential refresh: ground control returned an empty secret")
	}
	return RefreshResult{Secret: out.Secret}, nil
}

func ztrResult(out groundcontrol.StateConfigResponse) ZTRResult {
	return ZTRResult{
		StateURL: out.State,
		Robot:    RobotAccount{RegistryURL: out.Auth.URL, Username: out.Auth.Username, Password: out.Auth.Password},
		Session:  session(out.Session),
	}
}

func session(s *groundcontrol.SatelliteSession) *Session {
	if s == nil {
		return nil
	}
	return &Session{Token: s.Token, ExpiresAt: s.ExpiresAt}
}

// decode fills out from a 200 body, or returns the error the status maps to.
func decode(resp *http.Response, out any) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return statusError(resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// statusError keeps only Ground Control's error message, never the raw
// body, so a response cannot carry a secret into the satellite's logs.
func statusError(code int, body []byte) error {
	var appErr groundcontrol.AppError
	msg := http.StatusText(code)
	if json.Unmarshal(body, &appErr) == nil && appErr.Message != "" {
		msg = appErr.Message
	}
	switch code {
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ErrUnauthorized, msg)
	case http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrForbidden, msg)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w: %s", ErrRateLimited, msg)
	default:
		return &StatusError{StatusCode: code, Message: msg}
	}
}

type CachedImageReport = groundcontrol.CachedImageReport
