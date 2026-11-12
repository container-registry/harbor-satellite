package webhook_test

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/webhook"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

type plane struct {
	subs   *subStore
	log    *logReader
	locks  *locker
	sender *sender
	cfg    webhook.DispatcherConfig
}

func newPlane(entries ...webhook.LogEntry) *plane {
	return &plane{
		subs:   &subStore{due: []webhook.DueSubscription{{ID: 1, URL: "https://hooks.example/a", SealedSecret: []byte("sealed:s3cret")}}},
		log:    &logReader{entries: entries},
		locks:  &locker{held: map[fleet.WebhookID]bool{}},
		sender: &sender{fail: map[string]bool{}},
		cfg:    webhook.DefaultDispatcherConfig(),
	}
}

func (p *plane) runOnce(t *testing.T) int {
	t.Helper()
	d, err := webhook.NewDispatcher(p.subs, p.log, p.locks, sealer{}, p.sender, p.cfg)
	require.NoError(t, err)
	n, err := d.RunOnce(context.Background())
	require.NoError(t, err)
	return n
}

func TestDispatcher_DeliversInOrderAndAdvancesCheckpoint(t *testing.T) {
	pending.Plan(t, "9.1", pendingWebhooks)
	p := newPlane(entry(10, 3, fleet.EventGroupChanged), entry(11, 1, fleet.EventGroupChanged), entry(11, 2, fleet.EventConfigChanged))

	require.Equal(t, 3, p.runOnce(t))

	require.Equal(t, []string{"3", "1", "2"}, p.sender.sent, "(xid, id) order")
	require.Equal(t, []byte("s3cret"), p.sender.secrets[0], "the opened secret signs")
	require.Equal(t, webhook.Position{XID: 11, ID: 2}, p.subs.advanced[1])
}

func TestDispatcher_RetriesWithoutSkipping(t *testing.T) {
	pending.Plan(t, "9.1", pendingWebhooks)
	p := newPlane(entry(10, 1, fleet.EventGroupChanged), entry(10, 2, fleet.EventGroupChanged), entry(10, 3, fleet.EventGroupChanged))
	p.sender.fail["2"] = true

	require.Equal(t, 1, p.runOnce(t))

	require.Equal(t, []string{"1", "2"}, p.sender.sent, "nothing after a failure is sent")
	require.Equal(t, webhook.Position{XID: 10, ID: 1}, p.subs.advanced[1])
	require.Len(t, p.subs.failures, 1)
	require.Equal(t, 500, p.subs.failures[0].status)
}

func TestDispatcher_FilteredKindsAdvanceWithoutSend(t *testing.T) {
	pending.Plan(t, "9.1", pendingWebhooks)
	p := newPlane(entry(10, 1, fleet.EventGroupChanged), entry(10, 2, fleet.EventEdgeReplicated), entry(10, 3, fleet.EventGroupChanged), entry(10, 4, fleet.EventEdgeReplicated))
	p.subs.due[0].EventTypes = []fleet.EventKind{fleet.EventGroupChanged}

	require.Equal(t, 2, p.runOnce(t))

	require.Equal(t, []string{"1", "3"}, p.sender.sent)
	require.Equal(t, webhook.Position{XID: 10, ID: 4}, p.subs.advanced[1], "filtered events still move the checkpoint")
}

func TestDispatcher_SkipsSubscriptionLockedElsewhere(t *testing.T) {
	pending.Plan(t, "9.1", pendingWebhooks)
	p := newPlane(entry(10, 1, fleet.EventGroupChanged))
	p.locks.held[1] = true

	require.Zero(t, p.runOnce(t))

	require.Empty(t, p.sender.sent)
	require.Empty(t, p.subs.advanced)
}

func TestDispatcher_DisablesAfterContinuousFailure(t *testing.T) {
	pending.Plan(t, "9.1", pendingWebhooks)
	p := newPlane(entry(10, 1, fleet.EventGroupChanged))
	p.sender.always = 503
	p.subs.disable = true

	require.Zero(t, p.runOnce(t))

	require.Len(t, p.subs.failures, 1)
	require.Equal(t, p.cfg.DisableAfter, p.subs.failures[0].disableAfter)
	require.Positive(t, p.subs.failures[0].retryIn)
	require.Empty(t, p.subs.advanced)
}
