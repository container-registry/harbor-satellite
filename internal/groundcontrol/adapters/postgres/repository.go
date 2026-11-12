// Package postgres implements the fleet persistence ports with the sqlc
// queries of internal/groundcontrol/database (plan item 1.3).
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ fleet.Repository = (*Repository)(nil)

type Repository struct {
	db *sql.DB
	q  *database.Queries
}

func NewRepository(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: the postgres repository needs a database", fleet.ErrInvalid)
	}
	return &Repository{db: db, q: database.New(db)}, nil
}

func (r *Repository) Autocommit() fleet.Tx {
	return tx{q: r.q, autocommit: true}
}

// WithTx rolls back when fn fails or panics.
func (r *Repository) WithTx(ctx context.Context, fn func(tx fleet.Tx) error) (err error) {
	sqlTx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rbErr := sqlTx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("roll back: %w", rbErr))
		}
	}()
	if err := fn(tx{q: r.q.WithTx(sqlTx)}); err != nil {
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	committed = true
	return nil
}

type tx struct {
	q          *database.Queries
	autocommit bool
}

func (t tx) Satellites() fleet.SatelliteRepo { return satelliteRepo{q: t.q} }
func (t tx) Groups() fleet.GroupRepo         { return groupRepo{q: t.q} }
func (t tx) Configs() fleet.ConfigRepo       { return configRepo{q: t.q} }
func (t tx) Robots() fleet.RobotRepo         { return robotRepo{q: t.q} }
func (t tx) Tokens() fleet.TokenRepo         { return tokenRepo{q: t.q} }

func (t tx) Events() fleet.EventSink {
	if t.autocommit {
		return fleet.AutocommitEvents{}
	}
	return &OutboxSink{q: t.q}
}

func (t tx) Journal() fleet.JournalRepo { return journalRepo{q: t.q} }

func (tx) Views() fleet.ViewRepo      { return viewRepo{} }
func (tx) Policies() fleet.PolicyRepo { return policyRepo{} }
