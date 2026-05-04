package database

import (
	"context"
	"fmt"
	"strings"
)

type ListSatellitesFilteredParams struct {
	Limit          int32
	Offset         int32
	Sort           string
	Order          string
	NamePrefix     string
	LabelSelectors map[string]string
}

func normalizeSortOrder(sort, order string) (string, string) {
	sortColumn := map[string]string{
		"id":         "id",
		"name":       "name",
		"created_at": "created_at",
		"updated_at": "updated_at",
		"last_seen":  "last_seen",
	}[sort]
	if sortColumn == "" {
		sortColumn = "name"
	}
	orderUpper := strings.ToUpper(order)
	if orderUpper != "DESC" {
		orderUpper = "ASC"
	}
	return sortColumn, orderUpper
}

func (q *Queries) ListSatellitesFiltered(ctx context.Context, arg ListSatellitesFilteredParams) ([]Satellite, int32, error) {
	sortColumn, order := normalizeSortOrder(arg.Sort, arg.Order)

	whereSQL, args := satelliteListWhere(arg)
	countQuery := "SELECT COUNT(*) FROM satellites" + whereSQL

	var total int32
	if err := q.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	queryArgs := append(args, arg.Limit, arg.Offset)
	listQuery := fmt.Sprintf(`SELECT id, name, created_at, updated_at, last_seen, heartbeat_interval
FROM satellites%s
ORDER BY %s %s, id ASC
LIMIT $%d OFFSET $%d`, whereSQL, sortColumn, order, limitArg, offsetArg)

	rows, err := q.db.QueryContext(ctx, listQuery, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]Satellite, 0)
	for rows.Next() {
		var i Satellite
		if err := rows.Scan(
			&i.ID,
			&i.Name,
			&i.CreatedAt,
			&i.UpdatedAt,
			&i.LastSeen,
			&i.HeartbeatInterval,
		); err != nil {
			return nil, 0, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

func satelliteListWhere(arg ListSatellitesFilteredParams) (string, []any) {
	var clauses []string
	var args []any

	if arg.NamePrefix != "" {
		args = append(args, escapePostgresLikePattern(arg.NamePrefix))
		clauses = append(clauses, fmt.Sprintf("lower(name) LIKE lower($%d) || '%%' ESCAPE '\\'", len(args)))
	}

	if len(arg.LabelSelectors) > 0 {
		clause, extra := labelSelectorClause(arg.LabelSelectors, len(args)+1)
		args = append(args, extra...)
		clauses = append(clauses, clause)
	}

	if len(clauses) == 0 {
		return "", args
	}

	return " WHERE " + strings.Join(clauses, " AND "), args
}

func escapePostgresLikePattern(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return value
}
