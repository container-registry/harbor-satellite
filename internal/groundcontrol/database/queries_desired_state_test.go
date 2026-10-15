package database_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/stretchr/testify/require"
)

func TestRecordDesiredState_BumpsOnlyOnChange(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	record := func(aggregateType string, digest string) database.RecordDesiredStateRow {
		t.Helper()
		row, err := q.RecordDesiredState(ctx, database.RecordDesiredStateParams{
			AggregateType:  aggregateType,
			AggregateID:    7,
			Document:       json.RawMessage(`{"digest":"` + digest + `"}`),
			RenderedDigest: digest,
		})
		require.NoError(t, err)
		return row
	}
	publish := func(digest string) int64 {
		t.Helper()
		n, err := q.MarkDesiredStatePublished(ctx, database.MarkDesiredStatePublishedParams{AggregateType: "satellite", AggregateID: 7, Digest: digest, ArtifactDigest: "sha256:manifest-" + digest})
		require.NoError(t, err)
		return n
	}

	require.Equal(t, database.RecordDesiredStateRow{Version: 1, Publish: true}, record("satellite", "sha256:a"))
	require.Equal(t, database.RecordDesiredStateRow{Version: 1, Publish: true}, record("satellite", "sha256:a"), "same digest, still unpublished")
	require.EqualValues(t, 1, publish("sha256:a"))
	require.Equal(t, database.RecordDesiredStateRow{Version: 1, Publish: false}, record("satellite", "sha256:a"), "a row comes back even when nothing changed")
	require.Equal(t, database.RecordDesiredStateRow{Version: 2, Publish: true}, record("satellite", "sha256:b"))
	require.Zero(t, publish("sha256:a"), "a stale digest is not marked published")
	require.Equal(t, database.RecordDesiredStateRow{Version: 1, Publish: true}, record("group", "sha256:a"), "aggregates are keyed by type and ID")

	require.NoError(t, q.MarkDesiredStateFailed(ctx, database.MarkDesiredStateFailedParams{AggregateType: "satellite", AggregateID: 7, LastError: "registry down"}))
	got, err := q.GetDesiredState(ctx, database.GetDesiredStateParams{AggregateType: "satellite", AggregateID: 7})
	require.NoError(t, err)
	require.EqualValues(t, 2, got.Version)
	require.Equal(t, "sha256:b", got.RenderedDigest)
	require.Equal(t, "sha256:a", got.PublishedDigest.String)
	require.Equal(t, "registry down", got.LastError.String)
	require.JSONEq(t, `{"digest":"sha256:b"}`, string(got.Document))

	require.EqualValues(t, 1, publish("sha256:b"))
	got, err = q.GetDesiredState(ctx, database.GetDesiredStateParams{AggregateType: "satellite", AggregateID: 7})
	require.NoError(t, err)
	require.False(t, got.LastError.Valid, "publishing clears the error")
	require.True(t, got.PublishedAt.Valid)
	require.Equal(t, "sha256:manifest-sha256:b", got.PublishedArtifactDigest.String)

	require.NoError(t, q.ClearDesiredStatePublished(ctx, database.ClearDesiredStatePublishedParams{AggregateType: "satellite", AggregateID: 7}))
	require.Equal(t, database.RecordDesiredStateRow{Version: 2, Publish: true}, record("satellite", "sha256:b"), "a cleared row publishes again")

	require.NoError(t, q.DeleteDesiredState(ctx, database.DeleteDesiredStateParams{AggregateType: "satellite", AggregateID: 7}))
	_, err = q.GetDesiredState(ctx, database.GetDesiredStateParams{AggregateType: "satellite", AggregateID: 7})
	require.True(t, errors.Is(err, sql.ErrNoRows), "%v", err)
}

// The reconciler renders a group from group_artifacts and records the
// rendering later; an API write in between must survive (A81).
func TestGroupArtifacts_SurviveAnOlderRendering(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	group, err := q.CreateGroup(ctx, database.CreateGroupParams{GroupName: "g1", RegistryUrl: "http://harbor.example", Projects: []string{"library"}})
	require.NoError(t, err)
	v1 := json.RawMessage(`[{"repository":"library/alpine","tag":["3.20"]}]`)
	v2 := json.RawMessage(`[{"repository":"library/alpine","tag":["3.21"]}]`)
	require.NoError(t, q.UpsertGroupArtifacts(ctx, database.UpsertGroupArtifactsParams{GroupID: group.ID, Artifacts: v1}))

	rendered, err := q.GetGroupArtifacts(ctx, group.ID)
	require.NoError(t, err)
	require.NoError(t, q.UpsertGroupArtifacts(ctx, database.UpsertGroupArtifactsParams{GroupID: group.ID, Artifacts: v2}))
	_, err = q.RecordDesiredState(ctx, database.RecordDesiredStateParams{AggregateType: "group", AggregateID: int64(group.ID), Document: rendered, RenderedDigest: "sha256:v1"})
	require.NoError(t, err)

	got, err := q.GetGroupArtifacts(ctx, group.ID)
	require.NoError(t, err)
	require.JSONEq(t, string(v2), string(got))
	require.NoError(t, q.DeleteGroup(ctx, group.ID))
	_, err = q.GetGroupArtifacts(ctx, group.ID)
	require.True(t, errors.Is(err, sql.ErrNoRows), "the artifacts go with the group: %v", err)
}
