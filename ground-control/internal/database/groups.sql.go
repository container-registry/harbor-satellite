// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.28.0
// source: groups.sql

package database

import (
	"context"

	"github.com/lib/pq"
)

const createGroup = `-- name: CreateGroup :one
INSERT INTO groups (group_name, registry_url, projects, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
  ON CONFLICT (group_name)
  DO UPDATE SET
  registry_url = EXCLUDED.registry_url,
  projects = EXCLUDED.projects,
  updated_at = NOW()
RETURNING id, group_name, registry_url, projects, created_at, updated_at
`

type CreateGroupParams struct {
	GroupName   string
	RegistryUrl string
	Projects    []string
}

func (q *Queries) CreateGroup(ctx context.Context, arg CreateGroupParams) (Group, error) {
	row := q.db.QueryRowContext(ctx, createGroup, arg.GroupName, arg.RegistryUrl, pq.Array(arg.Projects))
	var i Group
	err := row.Scan(
		&i.ID,
		&i.GroupName,
		&i.RegistryUrl,
		pq.Array(&i.Projects),
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const deleteGroup = `-- name: DeleteGroup :exec
DELETE FROM groups
WHERE id = $1
`

func (q *Queries) DeleteGroup(ctx context.Context, id int32) error {
	_, err := q.db.ExecContext(ctx, deleteGroup, id)
	return err
}

const getGroupByID = `-- name: GetGroupByID :one
SELECT id, group_name, registry_url, projects, created_at, updated_at FROM groups
WHERE id = $1
`

func (q *Queries) GetGroupByID(ctx context.Context, id int32) (Group, error) {
	row := q.db.QueryRowContext(ctx, getGroupByID, id)
	var i Group
	err := row.Scan(
		&i.ID,
		&i.GroupName,
		&i.RegistryUrl,
		pq.Array(&i.Projects),
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const getGroupByName = `-- name: GetGroupByName :one
SELECT id, group_name, registry_url, projects, created_at, updated_at FROM groups
WHERE group_name = $1
`

func (q *Queries) GetGroupByName(ctx context.Context, groupName string) (Group, error) {
	row := q.db.QueryRowContext(ctx, getGroupByName, groupName)
	var i Group
	err := row.Scan(
		&i.ID,
		&i.GroupName,
		&i.RegistryUrl,
		pq.Array(&i.Projects),
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const getProjectsOfGroup = `-- name: GetProjectsOfGroup :many
SELECT projects FROM groups
WHERE group_name = $1
`

func (q *Queries) GetProjectsOfGroup(ctx context.Context, groupName string) ([][]string, error) {
	rows, err := q.db.QueryContext(ctx, getProjectsOfGroup, groupName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items [][]string
	for rows.Next() {
		var projects []string
		if err := rows.Scan(pq.Array(&projects)); err != nil {
			return nil, err
		}
		items = append(items, projects)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const listGroups = `-- name: ListGroups :many
SELECT id, group_name, registry_url, projects, created_at, updated_at FROM groups
`

func (q *Queries) ListGroups(ctx context.Context) ([]Group, error) {
	rows, err := q.db.QueryContext(ctx, listGroups)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Group
	for rows.Next() {
		var i Group
		if err := rows.Scan(
			&i.ID,
			&i.GroupName,
			&i.RegistryUrl,
			pq.Array(&i.Projects),
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
