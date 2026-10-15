// Package postgres implements the fleet persistence ports with the sqlc
// queries of internal/groundcontrol/database (plan item 1.3).
package postgres

import (
	"context"
	"database/sql"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

var _ fleet.Repository = (*Repository)(nil)

type Repository struct{}

func NewRepository(db *sql.DB) (*Repository, error) {
	return nil, notimpl.Err("1.3 postgres repository")
}

func (r *Repository) Autocommit() fleet.Tx {
	return tx{autocommit: true}
}

// WithTx rolls back when fn fails or panics.
func (r *Repository) WithTx(ctx context.Context, fn func(tx fleet.Tx) error) error {
	return notimpl.Err("1.3 postgres transaction")
}

type tx struct {
	autocommit bool
}

func (tx) Satellites() fleet.SatelliteRepo { return satelliteRepo{} }
func (tx) Groups() fleet.GroupRepo         { return groupRepo{} }
func (tx) Configs() fleet.ConfigRepo       { return configRepo{} }
func (tx) Robots() fleet.RobotRepo         { return robotRepo{} }
func (tx) Tokens() fleet.TokenRepo         { return tokenRepo{} }

func (t tx) Events() fleet.EventSink {
	if t.autocommit {
		return fleet.AutocommitEvents{}
	}
	return fleet.NopEventSink{}
}

func (tx) Journal() fleet.JournalRepo { return journalRepo{} }
