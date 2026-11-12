package postgres_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/stretchr/testify/require"
)

// A push of a tracked tag moves the digest of every group that lists it.
func TestGroupService_FollowPushUpdatesStoredGroups(t *testing.T) {
	repo := newRepository(t)
	ctx := context.Background()
	tx := repo.Autocommit()
	for _, name := range []string{"tracks", "other-tag"} {
		tag := "latest"
		if name == "other-tag" {
			tag = "1.27"
		}
		_, err := tx.Groups().Upsert(ctx, fleet.Group{
			Name: name, RegistryURL: "http://harbor.example", Projects: []string{"library"},
			Artifacts: []fleet.Artifact{{Repository: "library/nginx", Tags: []string{tag}, Digest: "sha256:old", Type: "image"}},
		})
		require.NoError(t, err)
	}
	svc, err := fleet.NewServices(fleet.Deps{Repository: repo, Registry: fleettest.NewRegistry(nil), Settings: fleet.Settings{RegistryURL: "http://harbor.example"}})
	require.NoError(t, err)

	updated, err := svc.Groups.FollowPush(ctx, fleet.RegistryEvent{
		Type: fleet.RegistryArtifactPushed, Project: "library", Repository: "nginx", Tag: "latest", Digest: "sha256:new",
	})

	require.NoError(t, err)
	require.Equal(t, []string{"tracks"}, updated)
	g, err := tx.Groups().GetByName(ctx, "tracks")
	require.NoError(t, err)
	require.Equal(t, "sha256:new", g.Artifacts[0].Digest)
	g, err = tx.Groups().GetByName(ctx, "other-tag")
	require.NoError(t, err)
	require.Equal(t, "sha256:old", g.Artifacts[0].Digest)
}

// A satellite's groups, with their artifacts, in one statement however
// many groups it is in.
func TestSatelliteRepo_GroupsInOneStatement(t *testing.T) {
	db := dbtest.New(t)
	repo, err := postgres.NewRepository(db.DB)
	require.NoError(t, err)
	ctx := context.Background()
	tx := repo.Autocommit()
	sat, err := tx.Satellites().Create(ctx, "edge-01")
	require.NoError(t, err)
	for i := range 3 {
		g, upsertErr := tx.Groups().Upsert(ctx, fleet.Group{
			Name: fmt.Sprintf("g%d", i), RegistryURL: "http://harbor.example", Projects: []string{"library"},
			Artifacts: []fleet.Artifact{{Repository: fmt.Sprintf("library/app%d", i), Tags: []string{"v1"}, Digest: "sha256:abc"}},
		})
		require.NoError(t, upsertErr)
		require.NoError(t, tx.Groups().AddMember(ctx, g.ID, sat.ID))
	}
	legacy, err := tx.Groups().Upsert(ctx, fleet.Group{Name: "no-artifacts", RegistryURL: "http://harbor.example"})
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "DELETE FROM group_artifacts WHERE group_id = $1", int32(legacy.ID))
	require.NoError(t, err)
	require.NoError(t, tx.Groups().AddMember(ctx, legacy.ID, sat.ID))

	var groups []fleet.Group
	stmts := db.MeasureStatements(t, func() {
		groups, err = tx.Satellites().Groups(ctx, sat.ID)
		require.NoError(t, err)
	})

	require.EqualValues(t, 1, stmts.Total, stmts.String())
	require.Len(t, groups, 4)
	for i, g := range groups[:3] {
		require.Equal(t, fmt.Sprintf("g%d", i), g.Name)
		require.Equal(t, fmt.Sprintf("library/app%d", i), g.Artifacts[0].Repository)
	}
	require.Equal(t, "no-artifacts", groups[3].Name)
	require.Nil(t, groups[3].Artifacts, "a group stored before group_artifacts has none")
}

// Updating a row writes a new tuple at the end of the heap, so without an
// ORDER BY a sequential scan returns the updated group last.
func TestGroups_ListIsOrderedByName(t *testing.T) {
	repo := newRepository(t)
	ctx := context.Background()
	groups := repo.Autocommit().Groups()
	for _, name := range []string{"alpha", "bravo"} {
		_, err := groups.Upsert(ctx, fleet.Group{Name: name, RegistryURL: "http://harbor.example", Projects: []string{"library"}})
		require.NoError(t, err)
	}
	_, err := groups.Upsert(ctx, fleet.Group{Name: "alpha", RegistryURL: "http://harbor.example", Projects: []string{"tools"}})
	require.NoError(t, err)

	listed, err := groups.List(ctx)

	require.NoError(t, err)
	names := make([]string, 0, len(listed))
	for _, g := range listed {
		names = append(names, g.Name)
	}
	require.Equal(t, []string{"alpha", "bravo"}, names)
}
