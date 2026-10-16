package webhook_test

import (
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/webhook"
	"github.com/container-registry/harbor-satellite/internal/shared/edgejournal"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingWebhooks = "Ground Control delivers no events; there are no webhooks"

func TestEventType_Naming(t *testing.T) {
	require.Equal(t, "io.harborsatellite.groundcontrol.GroupChanged", webhook.EventType(fleet.EventGroupChanged))
	require.Equal(t, edgejournal.CloudEventArtifactSynchronized, webhook.EventType(fleet.EventEdgeReplicated), "ADR-0011's type")
	require.Equal(t, edgejournal.CloudEventArtifactDeleted, webhook.EventType(fleet.EventEdgeDeleted))
	require.Equal(t, webhook.TypePrefix+"EdgePullThrough", webhook.EventType(fleet.EventEdgePullThrough), "ADR-0011 defines none")
}

func TestNewCloudEvent_Envelope(t *testing.T) {
	pending.Plan(t, "9.1", pendingWebhooks)

	ev, err := webhook.NewCloudEvent(entry(900, 42, fleet.EventGroupChanged), "urn:test")

	require.NoError(t, err)
	require.Equal(t, "1.0", ev.SpecVersion)
	require.Equal(t, "42", ev.ID, "the outbox id is the idempotency key")
	require.Equal(t, "urn:test", ev.Source)
	require.Equal(t, "io.harborsatellite.groundcontrol.GroupChanged", ev.Type)
	require.Equal(t, "group/3", ev.Subject)
	require.True(t, webhookNow.Equal(ev.Time))
	require.Equal(t, "application/json", ev.DataContentType)
	require.Equal(t, webhook.SchemaBaseURL+"GroupChanged.json", ev.DataSchema)
	require.JSONEq(t, `{"name":"g1"}`, string(ev.Data))
}
