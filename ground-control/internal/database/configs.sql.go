// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.28.0
// source: configs.sql

package database

import (
	"context"
	"encoding/json"
)

const createConfig = `-- name: CreateConfig :one
INSERT INTO configs (config_name, registry_url, config, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
RETURNING id, config_name, registry_url, config, created_at, updated_at
`

type CreateConfigParams struct {
	ConfigName  string
	RegistryUrl string
	Config      json.RawMessage
}

func (q *Queries) CreateConfig(ctx context.Context, arg CreateConfigParams) (Config, error) {
	row := q.db.QueryRowContext(ctx, createConfig, arg.ConfigName, arg.RegistryUrl, arg.Config)
	var i Config
	err := row.Scan(
		&i.ID,
		&i.ConfigName,
		&i.RegistryUrl,
		&i.Config,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const deleteConfig = `-- name: DeleteConfig :exec
DELETE FROM configs
WHERE id = $1
`

func (q *Queries) DeleteConfig(ctx context.Context, id int32) error {
	_, err := q.db.ExecContext(ctx, deleteConfig, id)
	return err
}

const getConfigByID = `-- name: GetConfigByID :one
SELECT id, config_name, registry_url, config, created_at, updated_at FROM configs
WHERE id = $1
`

func (q *Queries) GetConfigByID(ctx context.Context, id int32) (Config, error) {
	row := q.db.QueryRowContext(ctx, getConfigByID, id)
	var i Config
	err := row.Scan(
		&i.ID,
		&i.ConfigName,
		&i.RegistryUrl,
		&i.Config,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const getConfigByName = `-- name: GetConfigByName :one
SELECT id, config_name, registry_url, config, created_at, updated_at FROM configs
WHERE config_name = $1
`

func (q *Queries) GetConfigByName(ctx context.Context, configName string) (Config, error) {
	row := q.db.QueryRowContext(ctx, getConfigByName, configName)
	var i Config
	err := row.Scan(
		&i.ID,
		&i.ConfigName,
		&i.RegistryUrl,
		&i.Config,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const getRawConfigByID = `-- name: GetRawConfigByID :one
SELECT config FROM configs
WHERE id = $1
`

func (q *Queries) GetRawConfigByID(ctx context.Context, id int32) (json.RawMessage, error) {
	row := q.db.QueryRowContext(ctx, getRawConfigByID, id)
	var config json.RawMessage
	err := row.Scan(&config)
	return config, err
}

const getRawConfigByName = `-- name: GetRawConfigByName :one
SELECT config FROM configs
WHERE config_name = $1
`

func (q *Queries) GetRawConfigByName(ctx context.Context, configName string) (json.RawMessage, error) {
	row := q.db.QueryRowContext(ctx, getRawConfigByName, configName)
	var config json.RawMessage
	err := row.Scan(&config)
	return config, err
}

const listConfigs = `-- name: ListConfigs :many
SELECT id, config_name, registry_url, config, created_at, updated_at FROM configs
`

func (q *Queries) ListConfigs(ctx context.Context) ([]Config, error) {
	rows, err := q.db.QueryContext(ctx, listConfigs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Config
	for rows.Next() {
		var i Config
		if err := rows.Scan(
			&i.ID,
			&i.ConfigName,
			&i.RegistryUrl,
			&i.Config,
			&i.CreatedAt,
			&i.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const updateConfig = `-- name: UpdateConfig :one
UPDATE configs
SET registry_url = $2,
    config = $3,
    updated_at = NOW()
WHERE config_name = $1
RETURNING id, config_name, registry_url, config, created_at, updated_at
`

type UpdateConfigParams struct {
	ConfigName  string
	RegistryUrl string
	Config      json.RawMessage
}

func (q *Queries) UpdateConfig(ctx context.Context, arg UpdateConfigParams) (Config, error) {
	row := q.db.QueryRowContext(ctx, updateConfig, arg.ConfigName, arg.RegistryUrl, arg.Config)
	var i Config
	err := row.Scan(
		&i.ID,
		&i.ConfigName,
		&i.RegistryUrl,
		&i.Config,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}
