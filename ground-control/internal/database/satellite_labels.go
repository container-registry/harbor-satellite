package database

import (
	"context"
	"fmt"
	"strings"
)

// GetLabelsBySatelliteID returns all labels for a satellite as an ordered key→value map.
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

// SetLabels replaces the full label set for a satellite atomically.
func (q *Queries) SetLabels(ctx context.Context, satelliteID int32, labels map[string]string) error {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `DELETE FROM satellite_labels WHERE satellite_id = $1`, satelliteID); err != nil {
		return err
	}
	for k, v := range labels {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO satellite_labels (satellite_id, key, value) VALUES ($1, $2, $3)`,
			satelliteID, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PatchLabels merges changes into the label set: nil value removes the key, non-nil upserts it.
func (q *Queries) PatchLabels(ctx context.Context, satelliteID int32, patch map[string]*string) error {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for k, v := range patch {
		if v == nil {
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM satellite_labels WHERE satellite_id = $1 AND key = $2`,
				satelliteID, k); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO satellite_labels (satellite_id, key, value) VALUES ($1, $2, $3)
			 ON CONFLICT (satellite_id, key) DO UPDATE SET value = EXCLUDED.value`,
			satelliteID, k, *v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// labelSelectorClause builds a WHERE sub-clause and args for AND-semantics label selectors.
// Returns ("", nil) when selectors is empty.
func labelSelectorClause(selectors map[string]string, startIdx int) (string, []any) {
	if len(selectors) == 0 {
		return "", nil
	}
	var orConds []string
	var args []any
	idx := startIdx
	for k, v := range selectors {
		args = append(args, k, v)
		orConds = append(orConds, fmt.Sprintf("(key = $%d AND value = $%d)", idx, idx+1))
		idx += 2
	}
	args = append(args, int32(len(selectors)))
	clause := fmt.Sprintf(
		"id IN (SELECT satellite_id FROM satellite_labels WHERE %s GROUP BY satellite_id HAVING COUNT(*) = $%d)",
		strings.Join(orConds, " OR "), idx)
	return clause, args
}
