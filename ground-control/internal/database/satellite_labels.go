package database

import (
	"context"
	"fmt"
	"strings"
)

// GetLabelsBySatelliteID returns all labels for a satellite as a key→value map.
func (q *Queries) GetLabelsBySatelliteID(ctx context.Context, satelliteID int32) (map[string]string, error) {
	const query = `SELECT key, value FROM satellite_labels WHERE satellite_id = $1 ORDER BY key`
	rows, err := q.db.QueryContext(ctx, query, satelliteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	labels := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		labels[k] = v
	}
	return labels, rows.Err()
}

// DeleteLabelsBySatelliteID removes all labels for a satellite.
// Call within a transaction via WithTx for atomicity.
func (q *Queries) DeleteLabelsBySatelliteID(ctx context.Context, satelliteID int32) error {
	_, err := q.db.ExecContext(ctx, `DELETE FROM satellite_labels WHERE satellite_id = $1`, satelliteID)
	return err
}

// UpsertLabel inserts or updates a single label.
// Call within a transaction via WithTx for atomicity.
func (q *Queries) UpsertLabel(ctx context.Context, satelliteID int32, key, value string) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO satellite_labels (satellite_id, key, value) VALUES ($1, $2, $3)
		 ON CONFLICT (satellite_id, key) DO UPDATE SET value = EXCLUDED.value`,
		satelliteID, key, value)
	return err
}

// DeleteLabel removes a single label by key.
// Call within a transaction via WithTx for atomicity.
func (q *Queries) DeleteLabel(ctx context.Context, satelliteID int32, key string) error {
	_, err := q.db.ExecContext(ctx,
		`DELETE FROM satellite_labels WHERE satellite_id = $1 AND key = $2`,
		satelliteID, key)
	return err
}

// labelSelectorClause builds a WHERE sub-clause and args for AND-semantics label selectors.
// A nil value means "key must exist"; a non-nil value means "key=value" equality.
// Returns ("", nil) when selectors is empty.
func labelSelectorClause(selectors map[string]*string, startIdx int) (string, []any) {
	if len(selectors) == 0 {
		return "", nil
	}
	var orConds []string
	var args []any
	idx := startIdx
	for k, v := range selectors {
		if v == nil {
			args = append(args, k)
			orConds = append(orConds, fmt.Sprintf("(key = $%d)", idx))
			idx++
		} else {
			args = append(args, k, *v)
			orConds = append(orConds, fmt.Sprintf("(key = $%d AND value = $%d)", idx, idx+1))
			idx += 2
		}
	}
	args = append(args, int32(len(selectors)))
	clause := fmt.Sprintf(
		"id IN (SELECT satellite_id FROM satellite_labels WHERE %s GROUP BY satellite_id HAVING COUNT(*) = $%d)",
		strings.Join(orConds, " OR "), idx)
	return clause, args
}
