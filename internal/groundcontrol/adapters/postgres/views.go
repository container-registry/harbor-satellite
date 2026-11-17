package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/advisorylock"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// viewRepo runs the satellite_view queries: GetSatelliteViewByID,
// GetSatelliteViewByName, ListSatelliteViewsByState, RevokeSatelliteView,
// UnrevokeSatelliteView, MarkStaleSatellites (after
// TryAdvisoryXactLock(advisorylock.StaleDetector) in the same transaction)
// and ListSatelliteInventory.
type viewRepo struct{ q *database.Queries }

func (r viewRepo) Get(ctx context.Context, id fleet.SatelliteID) (fleet.SatelliteView, error) {
	row, err := r.q.GetSatelliteViewByID(ctx, int32(id))
	if err != nil {
		return fleet.SatelliteView{}, mapError(err)
	}
	return toView(row)
}

func (r viewRepo) GetByName(ctx context.Context, name string) (fleet.SatelliteView, error) {
	row, err := r.q.GetSatelliteViewByName(ctx, name)
	if err != nil {
		return fleet.SatelliteView{}, mapError(err)
	}
	return toView(database.GetSatelliteViewByIDRow(row))
}

func (r viewRepo) ListByState(ctx context.Context, state fleet.LifecycleState) ([]fleet.SatelliteView, error) {
	rows, err := r.q.ListSatelliteViewsByState(ctx, string(state))
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]fleet.SatelliteView, 0, len(rows))
	for _, row := range rows {
		v, err := toView(database.GetSatelliteViewByIDRow(row))
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (r viewRepo) Revoke(ctx context.Context, id fleet.SatelliteID, now time.Time) (fleet.LifecycleState, error) {
	previous, err := r.q.RevokeSatelliteView(ctx, database.RevokeSatelliteViewParams{SatelliteID: int32(id), Now: now})
	if err != nil {
		return "", mapError(err)
	}
	return fleet.LifecycleState(previous), nil
}

func (r viewRepo) Unrevoke(ctx context.Context, id fleet.SatelliteID, now time.Time) (bool, error) {
	n, err := r.q.UnrevokeSatelliteView(ctx, database.UnrevokeSatelliteViewParams{SatelliteID: int32(id), Now: now})
	if err != nil {
		return false, mapError(err)
	}
	return n > 0, nil
}

func (r viewRepo) MarkStale(ctx context.Context, now time.Time, defaultThreshold, minThreshold time.Duration) ([]fleet.StaleSatellite, error) {
	locked, err := r.q.TryAdvisoryXactLock(ctx, advisorylock.StaleDetector)
	if err != nil {
		return nil, mapError(err)
	}
	if !locked {
		return nil, nil
	}
	rows, err := r.q.MarkStaleSatellites(ctx, database.MarkStaleSatellitesParams{
		Now: now, DefaultThresholdSeconds: defaultThreshold.Seconds(), MinThresholdSeconds: minThreshold.Seconds(),
	})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]fleet.StaleSatellite, 0, len(rows))
	for _, row := range rows {
		out = append(out, fleet.StaleSatellite{Satellite: fleet.SatelliteID(row.SatelliteID), Name: row.Name, LastSeen: nullTime(row.LastSeen)})
	}
	return out, nil
}

func (r viewRepo) Inventory(ctx context.Context, id fleet.SatelliteID) ([]fleet.InventoryItem, error) {
	rows, err := r.q.ListSatelliteInventory(ctx, int32(id))
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]fleet.InventoryItem, 0, len(rows))
	for _, a := range rows {
		out = append(out, fleet.InventoryItem{Reference: a.Reference, SizeBytes: a.SizeBytes})
	}
	return out, nil
}

type publishedGroupRow struct {
	Group                   string  `json:"group"`
	PublishedArtifactDigest *string `json:"published_artifact_digest"`
}

func toView(row database.GetSatelliteViewByIDRow) (fleet.SatelliteView, error) {
	var applied []fleet.AppliedState
	if len(row.AppliedStates) > 0 {
		if err := json.Unmarshal(row.AppliedStates, &applied); err != nil {
			return fleet.SatelliteView{}, fmt.Errorf("applied states of satellite %s: %w", row.Name, err)
		}
	}
	var groups []publishedGroupRow
	if len(row.PublishedGroups) > 0 {
		if err := json.Unmarshal(row.PublishedGroups, &groups); err != nil {
			return fleet.SatelliteView{}, fmt.Errorf("published groups of satellite %s: %w", row.Name, err)
		}
	}
	published := make([]fleet.PublishedGroup, 0, len(groups))
	for _, g := range groups {
		pg := fleet.PublishedGroup{Group: g.Group}
		if g.PublishedArtifactDigest != nil {
			pg.ArtifactDigest = fleet.Digest(*g.PublishedArtifactDigest)
		}
		published = append(published, pg)
	}
	return fleet.SatelliteView{
		Satellite: fleet.Satellite{
			ID: fleet.SatelliteID(row.ID), Name: row.Name, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			LastSeen: nullTime(row.LastSeen), HeartbeatInterval: row.HeartbeatInterval.String,
		},
		State:                   fleet.LifecycleState(row.State),
		LastActivity:            row.LastActivity.String,
		LastReportedAt:          nullTime(row.LastReportedAt),
		AppliedStateDigest:      fleet.Digest(row.AppliedStateDigest.String),
		AppliedConfigDigest:     fleet.Digest(row.AppliedConfigDigest.String),
		AppliedStates:           applied,
		PublishedGroups:         published,
		InventoryDigest:         fleet.Digest(row.InventoryDigest.String),
		LastReplicationError:    row.LastReplicationError.String,
		RevokedAt:               nullTime(row.RevokedAt),
		DesiredVersion:          row.DesiredVersion.Int64,
		RenderedDigest:          fleet.Digest(row.RenderedDigest.String),
		PublishedDigest:         fleet.Digest(row.PublishedDigest.String),
		PublishedArtifactDigest: fleet.Digest(row.PublishedArtifactDigest.String),
		PublishError:            row.PublishError.String,
		PublishedAt:             nullTime(row.PublishedAt),
		UpdatedAt:               nullTime(row.ViewUpdatedAt),
	}, nil
}
