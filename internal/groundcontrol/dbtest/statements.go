package dbtest

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Statements is what pg_stat_statements recorded for one database.
type Statements struct {
	// Calls maps a query key to its executions. sqlc queries are keyed by
	// their name ("GetSatelliteByName"), others by their normalised text.
	Calls map[string]int64
	Total int64
}

// String lists the queries by descending call count, one per line.
func (s Statements) String() string {
	keys := make([]string, 0, len(s.Calls))
	for k := range s.Calls {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if s.Calls[keys[i]] != s.Calls[keys[j]] {
			return s.Calls[keys[i]] > s.Calls[keys[j]]
		}
		return keys[i] < keys[j]
	})
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%6d  %s\n", s.Calls[k], k)
	}
	fmt.Fprintf(&b, "%6d  total", s.Total)
	return b.String()
}

// MeasureStatements resets the statistics of this database, runs op and
// returns the statements executed meanwhile by any connection to it.
func (d *DB) MeasureStatements(t testing.TB, op func()) Statements {
	t.Helper()
	d.ResetStatements(t)
	op()
	return d.Statements(t)
}

// ResetStatements clears the statistics of this database only, so tests in
// other databases on the same server are not disturbed.
func (d *DB) ResetStatements(t testing.TB) {
	t.Helper()
	const q = `SELECT pg_stat_statements_reset(0, (SELECT oid FROM pg_database WHERE datname = current_database()), 0)`
	if _, err := d.ExecContext(context.Background(), q); err != nil {
		t.Fatalf("reset pg_stat_statements (is it in shared_preload_libraries?): %v", err)
	}
}

// Statements returns the statistics recorded since the last reset, leaving
// out the queries that read or reset them.
func (d *DB) Statements(t testing.TB) Statements {
	t.Helper()
	const q = `SELECT query, calls FROM pg_stat_statements
WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
  AND query NOT LIKE '%pg_stat_statements%'`
	rows, err := d.QueryContext(context.Background(), q)
	if err != nil {
		t.Fatalf("read pg_stat_statements: %v", err)
	}
	defer func() { _ = rows.Close() }()

	out := Statements{Calls: map[string]int64{}}
	for rows.Next() {
		var query string
		var calls int64
		if err := rows.Scan(&query, &calls); err != nil {
			t.Fatalf("scan pg_stat_statements: %v", err)
		}
		out.Calls[statementKey(query)] += calls
		out.Total += calls
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read pg_stat_statements: %v", err)
	}
	return out
}

func statementKey(query string) string {
	if rest, ok := strings.CutPrefix(query, "-- name: "); ok {
		if name, _, ok := strings.Cut(rest, " "); ok {
			return name
		}
	}
	key := strings.Join(strings.Fields(query), " ")
	if len(key) > 80 {
		key = key[:77] + "..."
	}
	return key
}
