package database

import (
	"context"
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
