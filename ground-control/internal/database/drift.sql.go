package database

import (
	"context"
	"database/sql"
	"time"
)

type ConfigDigest struct {
	ConfigID  int32
	Digest    string
	UpdatedAt time.Time
}

type SatelliteDesiredState struct {
	SatelliteID          int32
	ExpectedStateDigest  sql.NullString
	ExpectedConfigDigest sql.NullString
	LastConvergedAt      sql.NullTime
	UpdatedAt            time.Time
}

const upsertConfigDigest = `-- name: UpsertConfigDigest :exec
INSERT INTO config_digests (config_id, digest, updated_at)
VALUES ($1, $2, NOW())
ON CONFLICT (config_id)
DO UPDATE SET digest = EXCLUDED.digest, updated_at = NOW()
`

type UpsertConfigDigestParams struct {
	ConfigID int32
	Digest   string
}

func (q *Queries) UpsertConfigDigest(ctx context.Context, arg UpsertConfigDigestParams) error {
	_, err := q.db.ExecContext(ctx, upsertConfigDigest, arg.ConfigID, arg.Digest)
	return err
}

const getConfigDigest = `-- name: GetConfigDigest :one
SELECT config_id, digest, updated_at FROM config_digests
WHERE config_id = $1
`

func (q *Queries) GetConfigDigest(ctx context.Context, configID int32) (ConfigDigest, error) {
	row := q.db.QueryRowContext(ctx, getConfigDigest, configID)
	var i ConfigDigest
	err := row.Scan(&i.ConfigID, &i.Digest, &i.UpdatedAt)
	return i, err
}

const upsertSatelliteDesiredState = `-- name: UpsertSatelliteDesiredState :exec
INSERT INTO satellite_desired_states (
    satellite_id, expected_state_digest, expected_config_digest, updated_at
)
VALUES ($1, $2, $3, NOW())
ON CONFLICT (satellite_id)
DO UPDATE SET
    expected_state_digest = EXCLUDED.expected_state_digest,
    expected_config_digest = EXCLUDED.expected_config_digest,
    updated_at = NOW()
`

type UpsertSatelliteDesiredStateParams struct {
	SatelliteID          int32
	ExpectedStateDigest  sql.NullString
	ExpectedConfigDigest sql.NullString
}

func (q *Queries) UpsertSatelliteDesiredState(ctx context.Context, arg UpsertSatelliteDesiredStateParams) error {
	_, err := q.db.ExecContext(
		ctx,
		upsertSatelliteDesiredState,
		arg.SatelliteID,
		arg.ExpectedStateDigest,
		arg.ExpectedConfigDigest,
	)
	return err
}

const getSatelliteDesiredState = `-- name: GetSatelliteDesiredState :one
SELECT satellite_id, expected_state_digest, expected_config_digest, last_converged_at, updated_at
FROM satellite_desired_states
WHERE satellite_id = $1
`

func (q *Queries) GetSatelliteDesiredState(ctx context.Context, satelliteID int32) (SatelliteDesiredState, error) {
	row := q.db.QueryRowContext(ctx, getSatelliteDesiredState, satelliteID)
	var i SatelliteDesiredState
	err := row.Scan(&i.SatelliteID, &i.ExpectedStateDigest, &i.ExpectedConfigDigest, &i.LastConvergedAt, &i.UpdatedAt)
	return i, err
}

const updateSatelliteDesiredConfigDigestForConfig = `-- name: UpdateSatelliteDesiredConfigDigestForConfig :exec
INSERT INTO satellite_desired_states (satellite_id, expected_config_digest, updated_at)
SELECT sc.satellite_id, $2, NOW()
FROM satellite_configs sc
WHERE sc.config_id = $1
ON CONFLICT (satellite_id)
DO UPDATE SET
    expected_config_digest = EXCLUDED.expected_config_digest,
    updated_at = NOW()
`

type UpdateSatelliteDesiredConfigDigestForConfigParams struct {
	ConfigID int32
	Digest   string
}

func (q *Queries) UpdateSatelliteDesiredConfigDigestForConfig(ctx context.Context, arg UpdateSatelliteDesiredConfigDigestForConfigParams) error {
	_, err := q.db.ExecContext(ctx, updateSatelliteDesiredConfigDigestForConfig, arg.ConfigID, arg.Digest)
	return err
}

const updateSatelliteLastConvergedAt = `-- name: UpdateSatelliteLastConvergedAt :exec
UPDATE satellite_desired_states
SET last_converged_at = $2,
    updated_at = NOW()
WHERE satellite_id = $1
`

type UpdateSatelliteLastConvergedAtParams struct {
	SatelliteID     int32
	LastConvergedAt time.Time
}

func (q *Queries) UpdateSatelliteLastConvergedAt(ctx context.Context, arg UpdateSatelliteLastConvergedAtParams) error {
	_, err := q.db.ExecContext(ctx, updateSatelliteLastConvergedAt, arg.SatelliteID, arg.LastConvergedAt)
	return err
}
