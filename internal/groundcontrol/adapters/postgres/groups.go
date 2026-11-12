package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// groupRepo keeps a group's artifacts in group_artifacts and its other
// fields in groups, whose columns the group endpoints serialize.
type groupRepo struct{ q *database.Queries }

func (r groupRepo) Upsert(ctx context.Context, group fleet.Group) (fleet.Group, error) {
	projects := group.Projects
	if projects == nil {
		projects = []string{}
	}
	row, err := r.q.CreateGroup(ctx, database.CreateGroupParams{GroupName: group.Name, RegistryUrl: group.RegistryURL, Projects: projects})
	if err != nil {
		return fleet.Group{}, mapError(err)
	}
	artifacts := group.Artifacts
	if artifacts == nil {
		artifacts = []fleet.Artifact{}
	}
	doc, err := json.Marshal(artifacts)
	if err != nil {
		return fleet.Group{}, fmt.Errorf("encode group artifacts: %w", err)
	}
	if err := r.q.UpsertGroupArtifacts(ctx, database.UpsertGroupArtifactsParams{GroupID: row.ID, Artifacts: doc}); err != nil {
		return fleet.Group{}, mapError(err)
	}
	out := toGroup(row)
	out.Artifacts = group.Artifacts
	return out, nil
}

func toGroup(g database.Group) fleet.Group {
	return fleet.Group{ID: fleet.GroupID(g.ID), Name: g.GroupName, RegistryURL: g.RegistryUrl, Projects: g.Projects, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt}
}

// withArtifacts reads the group's artifacts; a group stored before
// group_artifacts existed has none.
func (r groupRepo) withArtifacts(ctx context.Context, g database.Group) (fleet.Group, error) {
	out := toGroup(g)
	doc, err := r.q.GetGroupArtifacts(ctx, g.ID)
	if err != nil {
		if errors.Is(mapError(err), fleet.ErrNotFound) {
			return out, nil
		}
		return fleet.Group{}, mapError(err)
	}
	var artifacts []fleet.Artifact
	if err := json.Unmarshal(doc, &artifacts); err != nil {
		return fleet.Group{}, fmt.Errorf("decode artifacts of group %s: %w", g.GroupName, err)
	}
	if len(artifacts) > 0 {
		out.Artifacts = artifacts
	}
	return out, nil
}

func (r groupRepo) Get(ctx context.Context, id fleet.GroupID) (fleet.Group, error) {
	g, err := r.q.GetGroupByID(ctx, int32(id))
	if err != nil {
		return fleet.Group{}, mapError(err)
	}
	return r.withArtifacts(ctx, g)
}

func (r groupRepo) GetByName(ctx context.Context, name string) (fleet.Group, error) {
	g, err := r.q.GetGroupByName(ctx, name)
	if err != nil {
		return fleet.Group{}, mapError(err)
	}
	return r.withArtifacts(ctx, g)
}

// List leaves the artifacts out: the list endpoint never shows them.
func (r groupRepo) List(ctx context.Context) ([]fleet.Group, error) {
	rows, err := r.q.ListGroups(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]fleet.Group, 0, len(rows))
	for _, g := range rows {
		out = append(out, toGroup(g))
	}
	return out, nil
}

func (r groupRepo) Delete(ctx context.Context, id fleet.GroupID) error {
	return mapError(r.q.DeleteGroup(ctx, int32(id)))
}

func (r groupRepo) Members(ctx context.Context, id fleet.GroupID) ([]fleet.SatelliteID, error) {
	links, err := r.q.GroupSatelliteList(ctx, int32(id))
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]fleet.SatelliteID, 0, len(links))
	for _, l := range links {
		out = append(out, fleet.SatelliteID(l.SatelliteID))
	}
	return out, nil
}

func (r groupRepo) AddMember(ctx context.Context, id fleet.GroupID, satellite fleet.SatelliteID) error {
	return mapError(r.q.AddSatelliteToGroup(ctx, database.AddSatelliteToGroupParams{SatelliteID: int32(satellite), GroupID: int32(id)}))
}

func (r groupRepo) RemoveMember(ctx context.Context, id fleet.GroupID, satellite fleet.SatelliteID) error {
	return mapError(r.q.RemoveSatelliteFromGroup(ctx, database.RemoveSatelliteFromGroupParams{SatelliteID: int32(satellite), GroupID: int32(id)}))
}

// GroupsOfSatellites runs ListGroupNamesOfSatellites.
func (r groupRepo) GroupsOfSatellites(ctx context.Context, ids []fleet.SatelliteID) (map[fleet.SatelliteID][]string, error) {
	rows, err := r.q.ListGroupNamesOfSatellites(ctx, int32IDs(ids))
	if err != nil {
		return nil, mapError(err)
	}
	out := make(map[fleet.SatelliteID][]string, len(ids))
	for _, row := range rows {
		id := fleet.SatelliteID(row.SatelliteID)
		out[id] = append(out[id], row.GroupName)
	}
	return out, nil
}
