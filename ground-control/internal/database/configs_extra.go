package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

const updateConfigIfMatch = `
UPDATE configs
SET registry_url = $2,
    config = $3,
    updated_at = NOW()
WHERE config_name = $1
  AND id = $4
  AND created_at = $5
  AND updated_at = $6
RETURNING id, config_name, registry_url, config, created_at, updated_at
`

type UpdateConfigIfMatchParams struct {
	ConfigName        string
	RegistryUrl       string
	Config            json.RawMessage
	ExpectedID        int32
	ExpectedCreatedAt time.Time
	ExpectedUpdatedAt time.Time
}

func (q *Queries) UpdateConfigIfMatch(ctx context.Context, arg UpdateConfigIfMatchParams) (Config, error) {
	row := q.db.QueryRowContext(
		ctx,
		updateConfigIfMatch,
		arg.ConfigName,
		arg.RegistryUrl,
		arg.Config,
		arg.ExpectedID,
		arg.ExpectedCreatedAt,
		arg.ExpectedUpdatedAt,
	)

	var cfg Config
	err := row.Scan(
		&cfg.ID,
		&cfg.ConfigName,
		&cfg.RegistryUrl,
		&cfg.Config,
		&cfg.CreatedAt,
		&cfg.UpdatedAt,
	)
	return cfg, err
}

const deleteConfigIfMatch = `
DELETE FROM configs
WHERE id = $1
  AND updated_at = $2
`

type DeleteConfigIfMatchParams struct {
	ID                int32
	ExpectedUpdatedAt time.Time
}

func (q *Queries) DeleteConfigIfMatch(ctx context.Context, arg DeleteConfigIfMatchParams) error {
	result, err := q.db.ExecContext(
		ctx,
		deleteConfigIfMatch,
		arg.ID,
		arg.ExpectedUpdatedAt,
	)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}
