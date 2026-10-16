package webhook_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/webhook"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

var secret = []byte("whsec-0123456789abcdef0123456789abcdef")

func newSender() *webhook.HTTPSender {
	return webhook.NewHTTPSender(&http.Client{Timeout: 5 * time.Second}, fleettest.Clock{T: time.Unix(1760529600, 0)})
}

func cloudEvent() webhook.CloudEvent {
	return webhook.CloudEvent{SpecVersion: "1.0", ID: "42", Source: "urn:test", Type: webhook.EventType(fleet.EventGroupChanged), Time: webhookNow, DataContentType: "application/json", Data: []byte(`{"name":"g1"}`)}
}

func TestHTTPSender_RefusesRedirects(t *testing.T) {
	pending.Plan(t, "9.1", pendingWebhooks)
	var reached atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Add(1) }))
	t.Cleanup(target.Close)
	redirect := httptest.NewServer(http.RedirectHandler(target.URL, http.StatusFound))
	t.Cleanup(redirect.Close)

	status, err := newSender().Send(context.Background(), redirect.URL, secret, cloudEvent())

	require.NoError(t, err)
	require.Equal(t, http.StatusFound, status, "a redirect is a failed delivery")
	require.Zero(t, reached.Load())
}

func TestHTTPSender_RequiresHTTPSForNonLoopback(t *testing.T) {
	pending.Plan(t, "9.1", pendingWebhooks)
	receiver := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(receiver.Close)

	_, err := newSender().Send(context.Background(), "http://hooks.example/events", secret, cloudEvent())
	require.ErrorIs(t, err, webhook.ErrInsecureURL)
	status, err := newSender().Send(context.Background(), receiver.URL, secret, cloudEvent())
	require.NoError(t, err, "plain http to loopback is allowed")
	require.Equal(t, http.StatusOK, status)
}

func TestHTTPSender_RefusesLinkLocal(t *testing.T) {
	pending.Plan(t, "9.1", pendingWebhooks)
	for _, url := range []string{"https://169.254.169.254/latest/meta-data", "https://[fe80::1]/events"} {
		_, err := newSender().Send(context.Background(), url, secret, cloudEvent())
		require.ErrorIs(t, err, webhook.ErrBlockedAddress, url)
	}
}

func TestHTTPSender_SetsSignatureHeaders(t *testing.T) {
	pending.Plan(t, "9.1", pendingWebhooks)
	var header http.Header
	var body []byte
	receiver := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		header = r.Header.Clone()
		if b, err := io.ReadAll(r.Body); err == nil {
			body = b
		}
	}))
	t.Cleanup(receiver.Close)

	status, err := newSender().Send(context.Background(), receiver.URL, secret, cloudEvent())

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, webhook.ContentType, header.Get("Content-Type"))
	require.Equal(t, "1760529600", header.Get(webhook.TimestampHeader))
	signature := header.Get(webhook.SignatureHeader)
	require.True(t, strings.HasPrefix(signature, "sha256="), signature)
	require.Equal(t, webhook.Sign(secret, time.Unix(1760529600, 0), body), signature)
}
