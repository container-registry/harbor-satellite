package fleet

import (
	"context"
	"errors"
	"fmt"
)

// allowsAll is true for the default policy: with it no check can deny, so
// the services skip loading parent scopes and keep today's statements.
func (c *core) allowsAll() bool {
	_, ok := c.policy.(AllowAll)
	return ok
}

// authorize asks the policy about the principal of ctx. A denial is
// ErrForbidden; a policy failure is returned as is.
func (c *core) authorize(ctx context.Context, action Action, resource Resource) error {
	if c.allowsAll() {
		return nil
	}
	principal, _ := PrincipalFrom(ctx)
	d, err := c.policy.Authorize(ctx, principal, action, resource)
	if err != nil {
		return fmt.Errorf("authorize %s on %s %s: %w", action, resource.Type, resource.ID, err)
	}
	if !d.Allowed {
		return fmt.Errorf("%w: %s on %s %s", ErrForbidden, action, resource.Type, resource.ID)
	}
	return nil
}

type check struct {
	action   Action
	resource Resource
}

// authorizeAll asks about every check, so the audit trail of a denied
// request names each refused resource, and returns the first denial. A
// policy failure stops at once.
func (c *core) authorizeAll(ctx context.Context, checks ...check) error {
	var denied error
	for _, ch := range checks {
		err := c.authorize(ctx, ch.action, ch.resource)
		switch {
		case err == nil:
		case isForbidden(err):
			if denied == nil {
				denied = err
			}
		default:
			return err
		}
	}
	return denied
}

func groupResources(names []string) []Resource {
	out := make([]Resource, 0, len(names))
	for _, n := range names {
		out = append(out, Resource{Type: ResourceGroup, ID: n})
	}
	return out
}

func satelliteResource(name string, groups []string) Resource {
	return Resource{Type: ResourceSatellite, ID: name, Parents: groupResources(groups)}
}

// satelliteScope loads the satellite and its groups, one statement for
// the groups, so a check can use the groups as parents.
func (c *core) satelliteScope(ctx context.Context, tx Tx, name string) (Satellite, []string, error) {
	sat, err := tx.Satellites().GetByName(ctx, name)
	if err != nil {
		return Satellite{}, nil, fmt.Errorf("satellite %s: %w", name, err)
	}
	if c.allowsAll() {
		return sat, nil, nil
	}
	groups, err := tx.Groups().GroupsOfSatellites(ctx, []SatelliteID{sat.ID})
	if err != nil {
		return Satellite{}, nil, fmt.Errorf("groups of satellite %s: %w", name, err)
	}
	return sat, groups[sat.ID], nil
}

// authorizeSatellite checks action on the named satellite with its groups
// as parents, before the caller's transaction.
func (s *SatelliteService) authorizeSatellite(ctx context.Context, action Action, name string) error {
	if s.c.allowsAll() {
		return nil
	}
	_, groups, err := s.c.satelliteScope(ctx, s.c.read(), name)
	if err != nil {
		return err
	}
	return s.c.authorize(ctx, action, satelliteResource(name, groups))
}

// readableSatellites keeps the satellites the principal may read, loading
// the parents of all of them in one statement.
func (c *core) readableSatellites(ctx context.Context, sats []Satellite) ([]Satellite, error) {
	if all, err := c.listsAll(ctx, ResourceSatellite); all || err != nil {
		return sats, err
	}
	if len(sats) == 0 {
		return sats, nil
	}
	ids := make([]SatelliteID, len(sats))
	for i, sat := range sats {
		ids[i] = sat.ID
	}
	groups, err := c.read().Groups().GroupsOfSatellites(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("groups of satellites: %w", err)
	}
	out := make([]Satellite, 0, len(sats))
	for _, sat := range sats {
		err := c.authorize(ctx, ActionRead, satelliteResource(sat.Name, groups[sat.ID]))
		switch {
		case err == nil:
			out = append(out, sat)
		case isForbidden(err):
		default:
			return nil, err
		}
	}
	return out, nil
}

func (s *SatelliteService) readable(ctx context.Context, views []SatelliteView) ([]SatelliteView, error) {
	if s.c.allowsAll() {
		return views, nil
	}
	sats := make([]Satellite, len(views))
	for i, v := range views {
		sats[i] = v.Satellite
	}
	keep, err := s.c.readableSatellites(ctx, sats)
	if err != nil {
		return nil, err
	}
	allowed := make(map[SatelliteID]bool, len(keep))
	for _, sat := range keep {
		allowed[sat.ID] = true
	}
	out := views[:0]
	for _, v := range views {
		if allowed[v.Satellite.ID] {
			out = append(out, v)
		}
	}
	return out, nil
}

// listsAll asks whether the principal may list every resource of typ, an
// unscoped list; when it may not, a list keeps the items it may read.
func (c *core) listsAll(ctx context.Context, typ ResourceType) (bool, error) {
	err := c.authorize(ctx, ActionList, Resource{Type: typ})
	switch {
	case err == nil:
		return true, nil
	case isForbidden(err):
		return false, nil
	default:
		return false, err
	}
}

func isForbidden(err error) bool {
	return errors.Is(err, ErrForbidden)
}

// filterAllowed keeps the items whose resource the principal may read.
func filterAllowed[T any](ctx context.Context, c *core, typ ResourceType, items []T, resource func(T) Resource) ([]T, error) {
	if all, err := c.listsAll(ctx, typ); all || err != nil {
		return items, err
	}
	out := make([]T, 0, len(items))
	for _, it := range items {
		err := c.authorize(ctx, ActionRead, resource(it))
		switch {
		case err == nil:
			out = append(out, it)
		case isForbidden(err):
		default:
			return nil, err
		}
	}
	return out, nil
}
