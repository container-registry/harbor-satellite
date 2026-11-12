package async

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/crypto"
)

var (
	_ RoutedHandler = (*SatelliteReconciler)(nil)
	_ RoutedHandler = (*GroupReconciler)(nil)
	_ RoutedHandler = (*ConfigReconciler)(nil)
	_ RoutedHandler = (*TombstoneHandler)(nil)
	_ Handler       = (*Router)(nil)
)

// Every reconciler renders from the database whatever the event says,
// publishes only when the rendered digest differs from the published one,
// and returns nil when its aggregate no longer exists.

// publisher is what the reconcilers share: the ports, the desired state
// and whether the satellite project is known to exist.
type publisher struct {
	repo     fleet.Repository
	registry fleet.Registry
	desired  DesiredStateStore
	settings fleet.Settings
	project  atomic.Bool
}

func newPublisherCore(repo fleet.Repository, registry fleet.Registry, desired DesiredStateStore, settings fleet.Settings) (*publisher, error) {
	if repo == nil || registry == nil || desired == nil {
		return nil, fmt.Errorf("%w: a reconciler needs a repository, a registry and a desired state store", fleet.ErrInvalid)
	}
	return &publisher{repo: repo, registry: registry, desired: desired, settings: settings}, nil
}

// ensureProject creates the project of the state artifacts once per
// process.
func (p *publisher) ensureProject(ctx context.Context) error {
	if p.project.Load() {
		return nil
	}
	if err := p.registry.EnsureProject(ctx, fleet.SatelliteProject); err != nil {
		return fmt.Errorf("ensure project %s: %w", fleet.SatelliteProject, err)
	}
	p.project.Store(true)
	return nil
}

// publish records doc for agg and, while it is not the published
// rendering, runs push and marks the result published. A failure is
// recorded on the row and returned, so the event is retried.
func (p *publisher) publish(ctx context.Context, agg fleet.Aggregate, doc []byte, digest fleet.Digest, push func() (fleet.Digest, error)) error {
	_, needed, err := p.desired.Record(ctx, agg, doc, digest)
	if err != nil {
		return fmt.Errorf("record desired state of %s %d: %w", agg.Type, agg.ID, err)
	}
	if !needed {
		return nil
	}
	artifact, err := p.pushOnce(ctx, push)
	if err != nil {
		if merr := p.desired.MarkFailed(context.WithoutCancel(ctx), agg, err.Error()); merr != nil {
			err = errors.Join(err, merr)
		}
		return err
	}
	return p.desired.MarkPublished(ctx, agg, digest, artifact)
}

func (p *publisher) pushOnce(ctx context.Context, push func() (fleet.Digest, error)) (fleet.Digest, error) {
	if err := p.ensureProject(ctx); err != nil {
		return "", err
	}
	return push()
}

// SatelliteReconciler publishes a satellite's desired state
// (fleet.RenderSatelliteDesiredState): its robot may pull
// fleet.RobotProjects of its groups (this supersedes D5's helper), and its
// state artifact lists its groups and config, groupless included (D10).
// It renders with robotDisabled = (the view state is revoked), applies
// SetRobotAccess(projects, disabled), then PushState, so a revoked
// satellite's robot is disabled with its projects kept (plan item 4.3).
// For RobotRotated, Handle calls desired.ClearPublished first, so the
// promoted successor gets the projects; the desired document does not
// change, so the fleet is not republished (plan item 8.4).
// It creates the robot when the satellite has none, named
// fleet.RobotName; when the registry answers ErrAlreadyExists, an earlier
// attempt created it before it could be stored, so the robot of exactly
// that name is adopted.
type SatelliteReconciler struct{ p *publisher }

func NewSatelliteReconciler(repo fleet.Repository, registry fleet.Registry, desired DesiredStateStore, settings fleet.Settings) (*SatelliteReconciler, error) {
	p, err := newPublisherCore(repo, registry, desired, settings)
	if err != nil {
		return nil, err
	}
	return &SatelliteReconciler{p: p}, nil
}

func (r *SatelliteReconciler) Kinds() []fleet.EventKind {
	return []fleet.EventKind{
		fleet.EventSatelliteCreated, fleet.EventAssignmentChanged, fleet.EventSatelliteChanged,
		fleet.EventSatelliteRevoked, fleet.EventSatelliteUnrevoked, fleet.EventRobotRotated,
	}
}

func (r *SatelliteReconciler) Reconcile(ctx context.Context, id fleet.SatelliteID) error {
	p := r.p
	read := p.repo.Autocommit()
	sat, err := read.Satellites().Get(ctx, id)
	if errors.Is(err, fleet.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("satellite %d: %w", id, err)
	}
	groups, err := read.Satellites().Groups(ctx, id)
	if err != nil {
		return fmt.Errorf("groups of satellite %s: %w", sat.Name, err)
	}
	slices.SortFunc(groups, func(a, b fleet.Group) int { return cmp.Compare(a.Name, b.Name) })
	state := fleet.DesiredState{Satellite: id}
	var projects []string
	for _, g := range groups {
		ref, refErr := p.settings.GroupStateRef(g.Name)
		if refErr != nil {
			return refErr
		}
		state.GroupStates = append(state.GroupStates, ref)
		projects = append(projects, g.Projects...)
	}
	cfg, err := read.Satellites().Config(ctx, id)
	switch {
	case err == nil:
		if state.ConfigState, err = p.settings.ConfigStateRef(cfg.Name); err != nil {
			return err
		}
	case !errors.Is(err, fleet.ErrNotFound):
		return fmt.Errorf("config of satellite %s: %w", sat.Name, err)
	}
	view, err := read.Views().Get(ctx, id)
	if err != nil && !errors.Is(err, fleet.ErrNotFound) {
		return fmt.Errorf("lifecycle of satellite %s: %w", sat.Name, err)
	}
	disabled := view.State == fleet.StateRevoked
	robotProjects := fleet.RobotProjects(projects)
	robot, err := r.robot(ctx, sat, robotProjects)
	if err != nil {
		return err
	}
	doc, digest, err := fleet.RenderSatelliteDesiredState(state, robotProjects, disabled)
	if err != nil {
		return err
	}
	stateRef, err := p.settings.SatelliteStateRef(sat.Name)
	if err != nil {
		return err
	}
	agg := fleet.Aggregate{Type: fleet.AggregateSatellite, ID: int64(id)}
	return p.publish(ctx, agg, doc, digest, func() (fleet.Digest, error) {
		if err := p.registry.SetRobotAccess(ctx, robot.HarborID, robotProjects, disabled); err != nil {
			return "", fmt.Errorf("set robot access of %s: %w", sat.Name, err)
		}
		statePart, _, err := fleet.RenderSatelliteState(state)
		if err != nil {
			return "", err
		}
		return p.registry.PushState(ctx, stateRef, statePart)
	})
}

// robot returns the satellite's robot, creating it with projects when it
// has none and adopting one an earlier attempt created but did not store.
func (r *SatelliteReconciler) robot(ctx context.Context, sat fleet.Satellite, projects []string) (fleet.Robot, error) {
	p := r.p
	robot, err := p.repo.Autocommit().Robots().BySatellite(ctx, sat.ID)
	if err == nil {
		return robot, nil
	}
	if !errors.Is(err, fleet.ErrNotFound) {
		return fleet.Robot{}, fmt.Errorf("robot of satellite %s: %w", sat.Name, err)
	}
	if err = p.ensureProject(ctx); err != nil {
		return fleet.Robot{}, err
	}
	name := fleet.RobotName(sat.ID, sat.Name)
	creds, err := p.registry.CreateRobot(ctx, fleet.RobotSpec{Name: name, Projects: projects, Duration: p.settings.RobotDuration})
	if errors.Is(err, fleet.ErrAlreadyExists) {
		creds, err = p.registry.FindRobot(ctx, name)
	}
	if err != nil {
		return fleet.Robot{}, fmt.Errorf("create robot %s: %w", name, err)
	}
	var hash string
	if creds.Secret != "" {
		if hash, err = crypto.HashSecret(creds.Secret); err != nil {
			return fleet.Robot{}, fmt.Errorf("hash robot secret: %w", err)
		}
	}
	robot, err = p.repo.Autocommit().Robots().Add(ctx, fleet.Robot{
		Satellite: sat.ID, Name: creds.Name, HarborID: creds.HarborID, SecretHash: hash, ExpiresAt: creds.ExpiresAt,
	})
	if err != nil {
		return fleet.Robot{}, fmt.Errorf("store robot %s: %w", name, err)
	}
	return robot, nil
}

func (r *SatelliteReconciler) Handle(ctx context.Context, event OutboxEvent) error {
	if event.Kind == fleet.EventRobotRotated {
		if err := r.p.desired.ClearPublished(ctx, event.Aggregate); err != nil {
			return fmt.Errorf("clear published state of satellite %d: %w", event.Aggregate.ID, err)
		}
	}
	return r.Reconcile(ctx, fleet.SatelliteID(event.Aggregate.ID))
}

// GroupReconciler publishes a group's state artifact from group_artifacts,
// then appends one EventSatelliteChanged per member with
// EventSink.AppendToGroupMembers, so a large group fans out in one
// statement and each member is reconciled, retried and locked on its own.
type GroupReconciler struct{ p *publisher }

func NewGroupReconciler(repo fleet.Repository, registry fleet.Registry, desired DesiredStateStore, settings fleet.Settings) (*GroupReconciler, error) {
	p, err := newPublisherCore(repo, registry, desired, settings)
	if err != nil {
		return nil, err
	}
	return &GroupReconciler{p: p}, nil
}

func (r *GroupReconciler) Kinds() []fleet.EventKind {
	return []fleet.EventKind{fleet.EventGroupChanged}
}

// Reconcile fans out on every call, also when the state was already
// published: a crash between the push and the fan-out must not leave the
// members with the old projects.
func (r *GroupReconciler) Reconcile(ctx context.Context, id fleet.GroupID) error {
	p := r.p
	group, err := p.repo.Autocommit().Groups().Get(ctx, id)
	if errors.Is(err, fleet.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("group %d: %w", id, err)
	}
	doc, digest, err := fleet.RenderGroupState(group, p.settings.RegistryURL)
	if err != nil {
		return err
	}
	ref, err := p.settings.GroupStateRef(group.Name)
	if err != nil {
		return err
	}
	agg := fleet.Aggregate{Type: fleet.AggregateGroup, ID: int64(id)}
	if err := p.publish(ctx, agg, doc, digest, func() (fleet.Digest, error) {
		return p.registry.PushState(ctx, ref, doc)
	}); err != nil {
		return err
	}
	return p.repo.WithTx(ctx, func(tx fleet.Tx) error {
		return tx.Events().AppendToGroupMembers(ctx, id, fleet.EventSatelliteChanged)
	})
}

func (r *GroupReconciler) Handle(ctx context.Context, event OutboxEvent) error {
	return r.Reconcile(ctx, fleet.GroupID(event.Aggregate.ID))
}

// ConfigReconciler publishes a config's state artifact.
type ConfigReconciler struct{ p *publisher }

func NewConfigReconciler(repo fleet.Repository, registry fleet.Registry, desired DesiredStateStore, settings fleet.Settings) (*ConfigReconciler, error) {
	p, err := newPublisherCore(repo, registry, desired, settings)
	if err != nil {
		return nil, err
	}
	return &ConfigReconciler{p: p}, nil
}

func (r *ConfigReconciler) Kinds() []fleet.EventKind {
	return []fleet.EventKind{fleet.EventConfigChanged}
}

func (r *ConfigReconciler) Reconcile(ctx context.Context, id fleet.ConfigID) error {
	p := r.p
	cfg, err := p.repo.Autocommit().Configs().Get(ctx, id)
	if errors.Is(err, fleet.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("config %d: %w", id, err)
	}
	ref, err := p.settings.ConfigStateRef(cfg.Name)
	if err != nil {
		return err
	}
	doc := []byte(cfg.Document)
	agg := fleet.Aggregate{Type: fleet.AggregateConfig, ID: int64(id)}
	return p.publish(ctx, agg, doc, digestOf(doc), func() (fleet.Digest, error) {
		return p.registry.PushState(ctx, ref, doc)
	})
}

func (r *ConfigReconciler) Handle(ctx context.Context, event OutboxEvent) error {
	return r.Reconcile(ctx, fleet.ConfigID(event.Aggregate.ID))
}

// TombstoneHandler deletes what a delete event's payload names
// (fleet.SatelliteDeletedPayload, GroupDeletedPayload,
// ConfigDeletedPayload): robots, the SPIRE entries of the SPIFFE ID, the
// state artifact and the desired-state row. State refs and SPIFFE IDs are
// built from names, so when a live aggregate of the same name exists (it
// was deleted and created again) the handler leaves the artifact and the
// SPIRE entries alone and clears that aggregate's published digest
// instead, so it is published again. A deleted group's members get an
// EventSatelliteChanged each. Objects already gone count as deleted.
type TombstoneHandler struct {
	repo     fleet.Repository
	registry fleet.Registry
	identity fleet.WorkloadIdentity
	desired  DesiredStateStore
}

func NewTombstoneHandler(repo fleet.Repository, registry fleet.Registry, identity fleet.WorkloadIdentity, desired DesiredStateStore) (*TombstoneHandler, error) {
	if repo == nil || registry == nil || desired == nil {
		return nil, fmt.Errorf("%w: the tombstone handler needs a repository, a registry and a desired state store", fleet.ErrInvalid)
	}
	return &TombstoneHandler{repo: repo, registry: registry, identity: identity, desired: desired}, nil
}

func (h *TombstoneHandler) Kinds() []fleet.EventKind {
	return []fleet.EventKind{fleet.EventSatelliteDeleted, fleet.EventGroupDeleted, fleet.EventConfigDeleted}
}

func gone(err error) error {
	if errors.Is(err, fleet.ErrNotFound) {
		return nil
	}
	return err
}

func (h *TombstoneHandler) Handle(ctx context.Context, event OutboxEvent) error {
	if event.Kind == fleet.EventSatelliteDeleted {
		var p fleet.SatelliteDeletedPayload
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return fmt.Errorf("decode %s payload: %w", event.Kind, err)
		}
		return h.satellite(ctx, event.Aggregate, p)
	}
	if event.Kind == fleet.EventGroupDeleted {
		var p fleet.GroupDeletedPayload
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return fmt.Errorf("decode %s payload: %w", event.Kind, err)
		}
		return h.group(ctx, event.Aggregate, p)
	}
	if event.Kind == fleet.EventConfigDeleted {
		var p fleet.ConfigDeletedPayload
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return fmt.Errorf("decode %s payload: %w", event.Kind, err)
		}
		return h.config(ctx, event.Aggregate, p)
	}
	return fmt.Errorf("%s: %w", event.Kind, ErrNoHandler)
}

func (h *TombstoneHandler) satellite(ctx context.Context, agg fleet.Aggregate, p fleet.SatelliteDeletedPayload) error {
	for _, id := range p.RobotHarborIDs {
		if err := gone(h.registry.DeleteRobot(ctx, id)); err != nil {
			return fmt.Errorf("delete robot %d of %s: %w", id, p.Name, err)
		}
	}
	live, err := h.repo.Autocommit().Satellites().GetByName(ctx, p.Name)
	switch {
	case err == nil && int64(live.ID) != agg.ID:
		if clearErr := h.desired.ClearPublished(ctx, fleet.Aggregate{Type: fleet.AggregateSatellite, ID: int64(live.ID)}); clearErr != nil {
			return clearErr
		}
	case err == nil || errors.Is(err, fleet.ErrNotFound):
		if p.StateRef != (fleet.ArtifactRef{}) {
			if deleteErr := gone(h.registry.DeleteState(ctx, p.StateRef)); deleteErr != nil {
				return fmt.Errorf("delete state of %s: %w", p.Name, deleteErr)
			}
		}
		if p.SPIFFEID != "" && h.identity != nil {
			if deleteErr := h.identity.DeleteEntriesFor(ctx, p.SPIFFEID); deleteErr != nil {
				return fmt.Errorf("delete SPIRE entries of %s: %w", p.Name, deleteErr)
			}
		}
	default:
		return fmt.Errorf("satellite %s: %w", p.Name, err)
	}
	return gone(h.desired.Delete(ctx, agg))
}

func (h *TombstoneHandler) group(ctx context.Context, agg fleet.Aggregate, p fleet.GroupDeletedPayload) error {
	live, err := h.repo.Autocommit().Groups().GetByName(ctx, p.Name)
	if stateErr := h.stateOfName(ctx, live.ID != 0 && int64(live.ID) != agg.ID, err,
		fleet.Aggregate{Type: fleet.AggregateGroup, ID: int64(live.ID)}, p.StateRef); stateErr != nil {
		return fmt.Errorf("group %s: %w", p.Name, stateErr)
	}
	if err := gone(h.desired.Delete(ctx, agg)); err != nil {
		return err
	}
	var events []fleet.Event
	read := h.repo.Autocommit()
	for _, id := range p.Members {
		sat, err := read.Satellites().Get(ctx, id)
		if errors.Is(err, fleet.ErrNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("member %d of group %s: %w", id, p.Name, err)
		}
		events = append(events, fleet.Event{
			Kind: fleet.EventSatelliteChanged, Aggregate: fleet.Aggregate{Type: fleet.AggregateSatellite, ID: int64(id)},
			Payload: fleet.ChangedPayload{Name: sat.Name},
		})
	}
	if len(events) == 0 {
		return nil
	}
	return h.repo.WithTx(ctx, func(tx fleet.Tx) error { return tx.Events().Append(ctx, events...) })
}

func (h *TombstoneHandler) config(ctx context.Context, agg fleet.Aggregate, p fleet.ConfigDeletedPayload) error {
	live, err := h.repo.Autocommit().Configs().GetByName(ctx, p.Name)
	if stateErr := h.stateOfName(ctx, live.ID != 0 && int64(live.ID) != agg.ID, err,
		fleet.Aggregate{Type: fleet.AggregateConfig, ID: int64(live.ID)}, p.StateRef); stateErr != nil {
		return fmt.Errorf("config %s: %w", p.Name, stateErr)
	}
	return gone(h.desired.Delete(ctx, agg))
}

// stateOfName deletes the state artifact of a deleted aggregate, unless an
// aggregate of the same name lives again; that one publishes again.
func (h *TombstoneHandler) stateOfName(ctx context.Context, reborn bool, lookupErr error, live fleet.Aggregate, ref fleet.ArtifactRef) error {
	if lookupErr != nil && !errors.Is(lookupErr, fleet.ErrNotFound) {
		return lookupErr
	}
	if lookupErr == nil && reborn {
		return h.desired.ClearPublished(ctx, live)
	}
	if ref == (fleet.ArtifactRef{}) {
		return nil
	}
	return gone(h.registry.DeleteState(ctx, ref))
}
