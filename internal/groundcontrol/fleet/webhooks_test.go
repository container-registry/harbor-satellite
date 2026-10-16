package fleet_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingWebhooks = "Ground Control delivers no events; there are no webhooks"

var webhookSecret = bytes.Repeat([]byte("s"), fleet.MinWebhookSecretBytes)

// prefixSealer seals as "sealed:" + plaintext.
type prefixSealer struct{}

func (prefixSealer) Seal(p []byte) ([]byte, error) { return append([]byte("sealed:"), p...), nil }
func (prefixSealer) Open(c []byte) ([]byte, error) { return c[len("sealed:"):], nil }

func (w *world) webhookServices(t *testing.T, enabled bool) *fleet.Services {
	t.Helper()
	repo := fleettest.NewWebhookRepo(w.rec)
	repo.NextID = 5
	svc, err := fleet.NewServices(fleet.Deps{
		Repository: w.repo, Registry: w.registry, Webhooks: repo, Sealer: prefixSealer{},
		Clock: fleettest.Clock{T: now}, Settings: fleet.Settings{RegistryURL: registryURL, WebhooksEnabled: enabled},
	})
	require.NoError(t, err)
	return svc
}

func TestWebhookService_CreateValidates(t *testing.T) {
	for name, tc := range map[string]struct {
		url    string
		secret []byte
		kinds  []fleet.EventKind
	}{
		"plain http to a remote host": {url: "http://hooks.example/a", secret: webhookSecret},
		"not a URL":                   {url: "::", secret: webhookSecret},
		"short secret":                {url: "https://hooks.example/a", secret: webhookSecret[:31]},
		"unknown kind":                {url: "https://hooks.example/a", secret: webhookSecret, kinds: []fleet.EventKind{"Nope"}},
	} {
		t.Run(name, func(t *testing.T) {
			pending.Plan(t, "9.1", pendingWebhooks)
			w := newWorld()
			svc := w.webhookServices(t, true)

			_, err := svc.Webhooks.Create(context.Background(), fleet.WebhookSubscription{URL: tc.url, EventTypes: tc.kinds}, tc.secret)

			require.ErrorIs(t, err, fleet.ErrInvalid)
			require.Zero(t, w.rec.Count("Webhooks.Create"))
		})
	}

	t.Run("http to loopback", func(t *testing.T) {
		pending.Plan(t, "9.1", pendingWebhooks)
		w := newWorld()
		svc := w.webhookServices(t, true)

		sub, err := svc.Webhooks.Create(context.Background(), fleet.WebhookSubscription{URL: "http://127.0.0.1:9000/hook", EventTypes: []fleet.EventKind{fleet.EventGroupChanged}}, webhookSecret)

		require.NoError(t, err)
		require.Equal(t, fleet.WebhookID(5), sub.ID)
	})
}

func TestWebhookService_SealsSecret(t *testing.T) {
	pending.Plan(t, "9.1", pendingWebhooks)
	w := newWorld()
	svc := w.webhookServices(t, true)

	_, err := svc.Webhooks.Create(context.Background(), fleet.WebhookSubscription{URL: "https://hooks.example/a"}, webhookSecret)

	require.NoError(t, err)
	sealed := arg[[]byte](t, onlyCall(t, w.rec, "Webhooks.Create"), 1)
	require.Equal(t, append([]byte("sealed:"), webhookSecret...), sealed, "only the sealed secret is stored")
}

func TestWebhookService_DisabledFeatureIsConflict(t *testing.T) {
	pending.Plan(t, "9.1", pendingWebhooks)
	w := newWorld()
	svc := w.webhookServices(t, false)

	_, err := svc.Webhooks.Create(context.Background(), fleet.WebhookSubscription{URL: "https://hooks.example/a"}, webhookSecret)

	require.ErrorIs(t, err, fleet.ErrConflict)
	require.Zero(t, w.rec.Count("Webhooks.Create"))
}
