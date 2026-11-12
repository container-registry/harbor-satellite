package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

type satelliteRepo struct{ q *database.Queries }

func toSatellite(id int32, name string, created, updated time.Time, lastSeen sql.NullTime, interval sql.NullString, spiffeID string) fleet.Satellite {
	return fleet.Satellite{
		ID: fleet.SatelliteID(id), Name: name, SPIFFEID: spiffeID,
		CreatedAt: created, UpdatedAt: updated,
		LastSeen: lastSeen.Time, HeartbeatInterval: interval.String,
	}
}

func (r satelliteRepo) Create(ctx context.Context, name string) (fleet.Satellite, error) {
	s, err := r.q.CreateSatellite(ctx, name)
	if err != nil {
		return fleet.Satellite{}, mapError(err)
	}
	return toSatellite(s.ID, s.Name, s.CreatedAt, s.UpdatedAt, s.LastSeen, s.HeartbeatInterval, ""), nil
}

func (r satelliteRepo) Get(ctx context.Context, id fleet.SatelliteID) (fleet.Satellite, error) {
	s, err := r.q.GetFleetSatelliteByID(ctx, int32(id))
	if err != nil {
		return fleet.Satellite{}, mapError(err)
	}
	return toSatellite(s.ID, s.Name, s.CreatedAt, s.UpdatedAt, s.LastSeen, s.HeartbeatInterval, s.SpiffeID), nil
}

func (r satelliteRepo) GetByName(ctx context.Context, name string) (fleet.Satellite, error) {
	s, err := r.q.GetFleetSatelliteByName(ctx, name)
	if err != nil {
		return fleet.Satellite{}, mapError(err)
	}
	return toSatellite(s.ID, s.Name, s.CreatedAt, s.UpdatedAt, s.LastSeen, s.HeartbeatInterval, s.SpiffeID), nil
}

func (r satelliteRepo) List(ctx context.Context) ([]fleet.Satellite, error) {
	rows, err := r.q.ListFleetSatellites(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]fleet.Satellite, 0, len(rows))
	for _, s := range rows {
		out = append(out, toSatellite(s.ID, s.Name, s.CreatedAt, s.UpdatedAt, s.LastSeen, s.HeartbeatInterval, s.SpiffeID))
	}
	return out, nil
}

func (r satelliteRepo) Delete(ctx context.Context, id fleet.SatelliteID) error {
	return mapError(r.q.DeleteSatellite(ctx, int32(id)))
}

func (r satelliteRepo) Groups(ctx context.Context, id fleet.SatelliteID) ([]fleet.Group, error) {
	links, err := r.q.SatelliteGroupList(ctx, int32(id))
	if err != nil {
		return nil, mapError(err)
	}
	groups := groupRepo(r)
	out := make([]fleet.Group, 0, len(links))
	for _, l := range links {
		g, err := groups.Get(ctx, fleet.GroupID(l.GroupID))
		if err != nil {
			return nil, fmt.Errorf("group %d of satellite %d: %w", l.GroupID, id, err)
		}
		out = append(out, g)
	}
	return out, nil
}

// RobotGrants runs the set-based GetProjectsOfSatellites of D5.
func (r satelliteRepo) RobotGrants(ctx context.Context, ids []fleet.SatelliteID) ([]fleet.RobotGrant, error) {
	rows, err := r.q.GetProjectsOfSatellites(ctx, int32IDs(ids))
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]fleet.RobotGrant, 0, len(rows))
	for _, row := range rows {
		harborID, err := parseHarborID(row.RobotID)
		if err != nil {
			return nil, err
		}
		out = append(out, fleet.RobotGrant{Satellite: fleet.SatelliteID(row.SatelliteID), HarborID: harborID, RobotName: row.RobotName, Projects: row.Projects})
	}
	return out, nil
}

func (r satelliteRepo) SetConfig(ctx context.Context, id fleet.SatelliteID, config fleet.ConfigID) error {
	return mapError(r.q.SetSatelliteConfig(ctx, database.SetSatelliteConfigParams{SatelliteID: int32(id), ConfigID: int32(config)}))
}

func (r satelliteRepo) Config(ctx context.Context, id fleet.SatelliteID) (fleet.Config, error) {
	link, err := r.q.SatelliteConfig(ctx, int32(id))
	if err != nil {
		return fleet.Config{}, mapError(err)
	}
	return configRepo(r).Get(ctx, fleet.ConfigID(link.ConfigID))
}

// SetIdentity runs UpsertSatelliteIdentity, with an empty entry list for
// nil because the column is NOT NULL.
func (r satelliteRepo) SetIdentity(ctx context.Context, id fleet.SatelliteID, spiffeID string, entryIDs []string) error {
	if entryIDs == nil {
		entryIDs = []string{}
	}
	return mapError(r.q.UpsertSatelliteIdentity(ctx, database.UpsertSatelliteIdentityParams{SatelliteID: int32(id), SpiffeID: spiffeID, SpireEntryIds: entryIDs}))
}

func int32IDs[T ~int32](ids []T) []int32 {
	out := make([]int32, len(ids))
	for i, id := range ids {
		out[i] = int32(id)
	}
	return out
}
