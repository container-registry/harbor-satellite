package fleettest

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var (
	_ fleet.Repository    = (*Repository)(nil)
	_ fleet.Tx            = tx{}
	_ fleet.SatelliteRepo = (*SatelliteRepo)(nil)
	_ fleet.GroupRepo     = (*GroupRepo)(nil)
	_ fleet.ConfigRepo    = (*ConfigRepo)(nil)
	_ fleet.RobotRepo     = (*RobotRepo)(nil)
	_ fleet.TokenRepo     = (*TokenRepo)(nil)
)

// Repository is a recording fleet.Repository. Autocommit and WithTx hand
// out the same repositories, except that Autocommit's Events() rejects
// appends like the real one; WithTx records "WithTx" before fn and
// "Commit" or "Rollback" after it. Lookups answer from the maps the test seeds, and a
// missing entry is fleet.ErrNotFound.
type Repository struct {
	*Recorder
	Satellites *SatelliteRepo
	Groups     *GroupRepo
	Configs    *ConfigRepo
	Robots     *RobotRepo
	Tokens     *TokenRepo
	Events     *EventSink
	Journal    *JournalRepo
}

// NewRepository records into rec, or into a new Recorder when rec is nil.
func NewRepository(rec *Recorder) *Repository {
	rec = orNew(rec)
	return &Repository{
		Recorder:   rec,
		Satellites: &SatelliteRepo{rec: rec},
		Groups:     &GroupRepo{rec: rec},
		Configs:    &ConfigRepo{rec: rec},
		Robots:     &RobotRepo{rec: rec},
		Tokens:     &TokenRepo{rec: rec},
		Events:     NewEventSink(rec),
		Journal:    &JournalRepo{rec: rec},
	}
}

func (r *Repository) Autocommit() fleet.Tx {
	return tx{r: r, autocommit: true}
}

// WithTx fails without running fn when "WithTx" is set to fail; a failing
// "Commit" makes a successful fn fail.
func (r *Repository) WithTx(_ context.Context, fn func(tx fleet.Tx) error) error {
	if err := r.Record("WithTx"); err != nil {
		return err
	}
	if err := fn(tx{r: r}); err != nil {
		return errors.Join(err, r.Record("Rollback"))
	}
	return r.Record("Commit")
}

type tx struct {
	r          *Repository
	autocommit bool
}

func (t tx) Satellites() fleet.SatelliteRepo { return t.r.Satellites }
func (t tx) Groups() fleet.GroupRepo         { return t.r.Groups }
func (t tx) Configs() fleet.ConfigRepo       { return t.r.Configs }
func (t tx) Robots() fleet.RobotRepo         { return t.r.Robots }
func (t tx) Tokens() fleet.TokenRepo         { return t.r.Tokens }
func (t tx) Events() fleet.EventSink {
	if t.autocommit {
		return fleet.AutocommitEvents{}
	}
	return t.r.Events
}

func (t tx) Journal() fleet.JournalRepo { return t.r.Journal }

// lookup records the call and answers it from m.
func lookup[K comparable, V any](rec *Recorder, method string, m map[K]V, key K) (V, error) {
	var zero V
	if err := rec.Record(method, key); err != nil {
		return zero, err
	}
	v, ok := m[key]
	if !ok {
		return zero, fleet.ErrNotFound
	}
	return v, nil
}

// find records the call and answers it with the first value of m that
// matches.
func find[K comparable, V any](rec *Recorder, method string, arg any, m map[K]V, match func(V) bool) (V, error) {
	var zero V
	if err := rec.Record(method, arg); err != nil {
		return zero, err
	}
	for _, v := range m {
		if match(v) {
			return v, nil
		}
	}
	return zero, fleet.ErrNotFound
}

// values records the call and returns the values of m ordered by key.
func values[V any](rec *Recorder, method string, m map[string]V) ([]V, error) {
	if err := rec.Record(method); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]V, 0, len(keys))
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out, nil
}

// SatelliteRepo is a recording fleet.SatelliteRepo.
type SatelliteRepo struct {
	rec *Recorder
	// ByName answers Get, GetByName and List.
	ByName   map[string]fleet.Satellite
	GroupsOf map[fleet.SatelliteID][]fleet.Group
	// GrantsOf answers RobotGrants; satellites without an entry have no
	// robot and are left out.
	GrantsOf map[fleet.SatelliteID]fleet.RobotGrant
	ConfigOf map[fleet.SatelliteID]fleet.Config
	// NextID is the ID Create returns.
	NextID fleet.SatelliteID
}

func (s *SatelliteRepo) Create(_ context.Context, name string) (fleet.Satellite, error) {
	if err := s.rec.Record("Satellites.Create", name); err != nil {
		return fleet.Satellite{}, err
	}
	return fleet.Satellite{ID: s.NextID, Name: name}, nil
}

func (s *SatelliteRepo) Get(_ context.Context, id fleet.SatelliteID) (fleet.Satellite, error) {
	return find(s.rec, "Satellites.Get", id, s.ByName, func(v fleet.Satellite) bool { return v.ID == id })
}

func (s *SatelliteRepo) GetByName(_ context.Context, name string) (fleet.Satellite, error) {
	return lookup(s.rec, "Satellites.GetByName", s.ByName, name)
}

func (s *SatelliteRepo) List(context.Context) ([]fleet.Satellite, error) {
	return values(s.rec, "Satellites.List", s.ByName)
}

func (s *SatelliteRepo) Delete(_ context.Context, id fleet.SatelliteID) error {
	return s.rec.Record("Satellites.Delete", id)
}

func (s *SatelliteRepo) Groups(_ context.Context, id fleet.SatelliteID) ([]fleet.Group, error) {
	if err := s.rec.Record("Satellites.Groups", id); err != nil {
		return nil, err
	}
	return s.GroupsOf[id], nil
}

func (s *SatelliteRepo) RobotGrants(_ context.Context, ids []fleet.SatelliteID) ([]fleet.RobotGrant, error) {
	if err := s.rec.Record("Satellites.RobotGrants", ids); err != nil {
		return nil, err
	}
	var out []fleet.RobotGrant
	for _, id := range ids {
		if g, ok := s.GrantsOf[id]; ok {
			out = append(out, g)
		}
	}
	return out, nil
}

func (s *SatelliteRepo) SetConfig(_ context.Context, id fleet.SatelliteID, config fleet.ConfigID) error {
	return s.rec.Record("Satellites.SetConfig", id, config)
}

func (s *SatelliteRepo) Config(_ context.Context, id fleet.SatelliteID) (fleet.Config, error) {
	return lookup(s.rec, "Satellites.Config", s.ConfigOf, id)
}

// GroupRepo is a recording fleet.GroupRepo.
type GroupRepo struct {
	rec *Recorder
	// ByName answers Get, GetByName and List, and gives Upsert the ID of
	// an existing group.
	ByName    map[string]fleet.Group
	MembersOf map[fleet.GroupID][]fleet.SatelliteID
	// NextID is the ID Upsert returns for a group not in ByName.
	NextID fleet.GroupID
}

func (g *GroupRepo) Upsert(_ context.Context, group fleet.Group) (fleet.Group, error) {
	if err := g.rec.Record("Groups.Upsert", group); err != nil {
		return fleet.Group{}, err
	}
	group.ID = g.NextID
	if existing, ok := g.ByName[group.Name]; ok {
		group.ID = existing.ID
	}
	return group, nil
}

func (g *GroupRepo) Get(_ context.Context, id fleet.GroupID) (fleet.Group, error) {
	return find(g.rec, "Groups.Get", id, g.ByName, func(v fleet.Group) bool { return v.ID == id })
}

func (g *GroupRepo) GetByName(_ context.Context, name string) (fleet.Group, error) {
	return lookup(g.rec, "Groups.GetByName", g.ByName, name)
}

func (g *GroupRepo) List(context.Context) ([]fleet.Group, error) {
	return values(g.rec, "Groups.List", g.ByName)
}

func (g *GroupRepo) Delete(_ context.Context, id fleet.GroupID) error {
	return g.rec.Record("Groups.Delete", id)
}

func (g *GroupRepo) Members(_ context.Context, id fleet.GroupID) ([]fleet.SatelliteID, error) {
	if err := g.rec.Record("Groups.Members", id); err != nil {
		return nil, err
	}
	return g.MembersOf[id], nil
}

func (g *GroupRepo) AddMember(_ context.Context, id fleet.GroupID, satellite fleet.SatelliteID) error {
	return g.rec.Record("Groups.AddMember", id, satellite)
}

func (g *GroupRepo) RemoveMember(_ context.Context, id fleet.GroupID, satellite fleet.SatelliteID) error {
	return g.rec.Record("Groups.RemoveMember", id, satellite)
}

// ConfigRepo is a recording fleet.ConfigRepo.
type ConfigRepo struct {
	rec *Recorder
	// ByName answers Get, GetByName and List.
	ByName map[string]fleet.Config
	// InUseIDs answers InUse.
	InUseIDs map[fleet.ConfigID]bool
	// NextID is the ID Create returns.
	NextID fleet.ConfigID
}

func (c *ConfigRepo) Create(_ context.Context, config fleet.Config) (fleet.Config, error) {
	if err := c.rec.Record("Configs.Create", config); err != nil {
		return fleet.Config{}, err
	}
	config.ID = c.NextID
	return config, nil
}

func (c *ConfigRepo) Get(_ context.Context, id fleet.ConfigID) (fleet.Config, error) {
	return find(c.rec, "Configs.Get", id, c.ByName, func(v fleet.Config) bool { return v.ID == id })
}

func (c *ConfigRepo) GetByName(_ context.Context, name string) (fleet.Config, error) {
	return lookup(c.rec, "Configs.GetByName", c.ByName, name)
}

func (c *ConfigRepo) List(context.Context) ([]fleet.Config, error) {
	return values(c.rec, "Configs.List", c.ByName)
}

// Update returns config as given.
func (c *ConfigRepo) Update(_ context.Context, config fleet.Config) (fleet.Config, error) {
	if err := c.rec.Record("Configs.Update", config); err != nil {
		return fleet.Config{}, err
	}
	return config, nil
}

func (c *ConfigRepo) Delete(_ context.Context, id fleet.ConfigID) error {
	return c.rec.Record("Configs.Delete", id)
}

func (c *ConfigRepo) InUse(_ context.Context, id fleet.ConfigID) (bool, error) {
	if err := c.rec.Record("Configs.InUse", id); err != nil {
		return false, err
	}
	return c.InUseIDs[id], nil
}

// RobotRepo is a recording fleet.RobotRepo.
type RobotRepo struct {
	rec *Recorder
	// BySatelliteID answers BySatellite and ByName.
	BySatelliteID map[fleet.SatelliteID]fleet.Robot
	// NextID is the ID Add returns.
	NextID fleet.RobotID
}

func (r *RobotRepo) Add(_ context.Context, robot fleet.Robot) (fleet.Robot, error) {
	if err := r.rec.Record("Robots.Add", robot); err != nil {
		return fleet.Robot{}, err
	}
	robot.ID = r.NextID
	return robot, nil
}

func (r *RobotRepo) BySatellite(_ context.Context, satellite fleet.SatelliteID) (fleet.Robot, error) {
	return lookup(r.rec, "Robots.BySatellite", r.BySatelliteID, satellite)
}

func (r *RobotRepo) ByName(_ context.Context, name string) (fleet.Robot, error) {
	return find(r.rec, "Robots.ByName", name, r.BySatelliteID, func(v fleet.Robot) bool { return v.Name == name })
}

func (r *RobotRepo) Update(_ context.Context, robot fleet.Robot) error {
	return r.rec.Record("Robots.Update", robot)
}

func (r *RobotRepo) Delete(_ context.Context, id fleet.RobotID) error {
	return r.rec.Record("Robots.Delete", id)
}

// TokenRepo is a recording fleet.TokenRepo.
type TokenRepo struct {
	rec *Recorder
	// Claimable answers Claim; a token not in it is fleet.ErrTokenInvalid.
	// Inject fleet.ErrTokenExpired with FailOn("Tokens.Claim", ...).
	Claimable map[string]fleet.SatelliteID
}

func (t *TokenRepo) Add(_ context.Context, satellite fleet.SatelliteID, token string, expiresAt time.Time) error {
	return t.rec.Record("Tokens.Add", satellite, token, expiresAt)
}

func (t *TokenRepo) Claim(_ context.Context, token string, now time.Time) (fleet.SatelliteID, error) {
	if err := t.rec.Record("Tokens.Claim", token, now); err != nil {
		return 0, err
	}
	id, ok := t.Claimable[token]
	if !ok {
		return 0, fleet.ErrTokenInvalid
	}
	return id, nil
}

func (t *TokenRepo) Unclaim(_ context.Context, token string) error {
	return t.rec.Record("Tokens.Unclaim", token)
}

func (t *TokenRepo) Consume(_ context.Context, token string) error {
	return t.rec.Record("Tokens.Consume", token)
}
