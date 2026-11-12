package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/advisorylock"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ async.Job = (*PartitionMaintainer)(nil)

// PartitionMaintainer owns satellite_status retention since migration 024
// (plan item 4.5); the worker runs it. Partitions are UTC days named
// satellite_status_YYYYMMDD.
type PartitionMaintainer struct {
	db                       *sql.DB
	clock                    fleet.Clock
	retentionDays, daysAhead int
}

func NewPartitionMaintainer(db *sql.DB, clock fleet.Clock, retentionDays, daysAhead int) (*PartitionMaintainer, error) {
	if db == nil || retentionDays <= 0 || daysAhead < 0 {
		return nil, fmt.Errorf("%w: the partition maintainer needs a database, positive retention days and non-negative days ahead", fleet.ErrInvalid)
	}
	if clock == nil {
		clock = fleet.SystemClock{}
	}
	return &PartitionMaintainer{db: db, clock: clock, retentionDays: retentionDays, daysAhead: daysAhead}, nil
}

func (m *PartitionMaintainer) Name() string { return "status-partitions" }

// Run calls RunOnce at once and then hourly until ctx ends, so a
// replica that starts just before midnight still creates tomorrow's
// partition in time.
func (m *PartitionMaintainer) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if err := m.RunOnce(ctx); err != nil && ctx.Err() == nil {
			log.Printf("status partitions: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

var partitionName = regexp.MustCompile(`^satellite_status_(\d{8})$`)

// RunOnce takes advisorylock.StatusPartitions, creates satellite_status_YYYYMMDD for today and daysAhead, drops partitions older than retentionDays; a no-op while satellite_status is not partitioned.
func (m *PartitionMaintainer) RunOnce(ctx context.Context) (err error) {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err == nil {
			err = tx.Commit()
			return
		}
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			err = errors.Join(err, rbErr)
		}
	}()
	locked, err := database.New(tx).TryAdvisoryXactLock(ctx, advisorylock.StatusPartitions)
	if err != nil || !locked {
		return err
	}
	var kind string
	if err := tx.QueryRowContext(ctx, "SELECT relkind::TEXT FROM pg_class WHERE oid = to_regclass('satellite_status')").Scan(&kind); err != nil {
		return fmt.Errorf("inspect satellite_status: %w", err)
	}
	if kind != "p" {
		return nil
	}
	today := m.clock.Now().UTC().Truncate(24 * time.Hour)
	for d := 0; d <= m.daysAhead; d++ {
		if err := createDay(ctx, tx, today.AddDate(0, 0, d)); err != nil {
			return err
		}
	}
	return dropExpired(ctx, tx, today.AddDate(0, 0, -m.retentionDays))
}

// createDay creates the day's partition. Rows of that day already in the
// default partition move into it first, as PostgreSQL refuses to attach a
// range the default partition holds rows of.
func createDay(ctx context.Context, tx *sql.Tx, day time.Time) error {
	name := "satellite_status_" + day.Format("20060102")
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", name).Scan(&exists); err != nil {
		return fmt.Errorf("look up partition %s: %w", name, err)
	}
	if exists {
		return nil
	}
	from, to := day.Format(time.RFC3339), day.AddDate(0, 0, 1).Format(time.RFC3339)
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE %s (LIKE satellite_status INCLUDING DEFAULTS INCLUDING CONSTRAINTS)`, name),
		fmt.Sprintf(`WITH moved AS (DELETE FROM satellite_status_default WHERE created_at >= '%s' AND created_at < '%s' RETURNING *)
			INSERT INTO %s SELECT * FROM moved`, from, to, name),
		fmt.Sprintf(`ALTER TABLE satellite_status ATTACH PARTITION %s FOR VALUES FROM ('%s') TO ('%s')`, name, from, to),
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create partition %s: %w", name, err)
		}
	}
	return nil
}

// dropExpired drops the day partitions before cutoff and deletes the rows
// before cutoff that the default partition still holds.
func dropExpired(ctx context.Context, tx *sql.Tx, cutoff time.Time) error {
	expired, err := expiredPartitions(ctx, tx, cutoff)
	if err != nil {
		return err
	}
	for _, name := range expired {
		if _, err := tx.ExecContext(ctx, "DROP TABLE "+name); err != nil {
			return fmt.Errorf("drop partition %s: %w", name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM satellite_status_default WHERE created_at < $1", cutoff); err != nil {
		return fmt.Errorf("expire default partition: %w", err)
	}
	return nil
}

func expiredPartitions(ctx context.Context, tx *sql.Tx, cutoff time.Time) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT c.relname FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = 'satellite_status'::regclass`)
	if err != nil {
		return nil, fmt.Errorf("list partitions: %w", err)
	}
	defer rows.Close()
	var expired []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		match := partitionName.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		if day, err := time.Parse("20060102", match[1]); err == nil && day.Before(cutoff) {
			expired = append(expired, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list partitions: %w", err)
	}
	return expired, nil
}
