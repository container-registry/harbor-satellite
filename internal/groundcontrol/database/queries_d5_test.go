package database_test

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/stretchr/testify/require"
)

func TestGetProjectsOfSatellites_UnionAcrossGroups(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	satellites := map[string]int32{}
	for _, name := range []string{"two-groups", "one-group", "no-group", "not-asked", "no-robot"} {
		sat, err := q.CreateSatellite(ctx, name)
		require.NoError(t, err)
		satellites[name] = sat.ID
		if name == "no-robot" {
			continue
		}
		_, err = q.AddRobotAccount(ctx, database.AddRobotAccountParams{
			RobotName: "robot$" + name, RobotSecretHash: "hash", RobotID: "100" + name, SatelliteID: sat.ID,
		})
		require.NoError(t, err)
	}
	for _, g := range []struct {
		name       string
		projects   []string
		satellites []string
	}{
		{name: "g1", projects: []string{"a", "b"}, satellites: []string{"two-groups", "not-asked", "no-robot"}},
		{name: "g2", projects: []string{"c", "b"}, satellites: []string{"two-groups"}},
		{name: "g3", projects: []string{"d"}, satellites: []string{"one-group"}},
	} {
		group, err := q.CreateGroup(ctx, database.CreateGroupParams{GroupName: g.name, RegistryUrl: "http://harbor.test", Projects: g.projects})
		require.NoError(t, err)
		for _, sat := range g.satellites {
			require.NoError(t, q.AddSatelliteToGroup(ctx, database.AddSatelliteToGroupParams{SatelliteID: satellites[sat], GroupID: group.ID}))
		}
	}

	rows, err := q.GetProjectsOfSatellites(ctx, []int32{
		satellites["two-groups"], satellites["one-group"], satellites["no-group"], satellites["no-robot"],
	})

	require.NoError(t, err)
	require.Equal(t, []database.GetProjectsOfSatellitesRow{
		{SatelliteID: satellites["two-groups"], RobotID: "100two-groups", RobotName: "robot$two-groups", Projects: []string{"a", "b", "c"}},
		{SatelliteID: satellites["one-group"], RobotID: "100one-group", RobotName: "robot$one-group", Projects: []string{"d"}},
		{SatelliteID: satellites["no-group"], RobotID: "100no-group", RobotName: "robot$no-group", Projects: []string{}},
	}, rows)
}
