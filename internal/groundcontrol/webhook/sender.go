package webhook

import (
	"context"
	"errors"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

var (
	ErrInsecureURL = errors.New("webhook URL is http to a host that is not loopback")
	// ErrBlockedAddress covers the cloud metadata endpoints (ADR-0019).
	ErrBlockedAddress = errors.New("webhook URL resolves to a link-local address")
)

// HTTPSender posts the event with ContentType and the signature headers.
// It follows no redirects, refuses http for non-loopback hosts
// (ErrInsecureURL) and dials no link-local address (ErrBlockedAddress).
type HTTPSender struct{}

var _ Sender = (*HTTPSender)(nil)

func NewHTTPSender(client *http.Client, clock fleet.Clock) *HTTPSender { return &HTTPSender{} }

func (s *HTTPSender) Send(ctx context.Context, url string, secret []byte, event CloudEvent) (int, error) {
	return 0, notimpl.Err("9.1 webhook send")
}
