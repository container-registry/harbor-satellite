package fleet

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

type GroupService struct{ c *core }

type SyncGroupCmd struct {
	Name      string
	Artifacts []Artifact
}

type MembershipCmd struct {
	Satellite, Group string
}

// Sync stores the group with the distinct projects of its non-deleted
// artifacts and appends GroupChanged; the group reconciler pushes its
// state and fans out to the members (plan item 2.5). A failure leaves the
// group unchanged.
func (s *GroupService) Sync(ctx context.Context, cmd SyncGroupCmd) (Group, error) {
	if !ValidName(cmd.Name) {
		return Group{}, fmt.Errorf("%w: group name %q", ErrInvalid, cmd.Name)
	}
	group := Group{Name: cmd.Name, RegistryURL: s.c.settings.RegistryURL, Projects: ArtifactProjects(cmd.Artifacts), Artifacts: cmd.Artifacts}
	var stored Group
	err := s.c.inTx(ctx, func(tx Tx) error {
		var err error
		stored, err = tx.Groups().Upsert(ctx, group)
		if err != nil {
			return fmt.Errorf("store group %s: %w", cmd.Name, err)
		}
		return tx.Events().Append(ctx, groupEvent(EventGroupChanged, stored))
	})
	if err != nil {
		return Group{}, err
	}
	return stored, nil
}

// ArtifactProjects are the distinct registry projects of the artifacts
// that are not deleted, sorted.
func ArtifactProjects(artifacts []Artifact) []string {
	var projects []string
	for _, a := range artifacts {
		if a.Deleted {
			continue
		}
		project, _, _ := strings.Cut(a.Repository, "/")
		if project != "" {
			projects = append(projects, project)
		}
	}
	slices.Sort(projects)
	return slices.Compact(projects)
}

func groupEvent(kind EventKind, g Group) Event {
	return Event{Kind: kind, Aggregate: Aggregate{Type: AggregateGroup, ID: int64(g.ID)}, Payload: ChangedPayload{Name: g.Name}}
}

func (s *GroupService) Delete(ctx context.Context, name string) error {
	stateRef, err := s.c.settings.GroupStateRef(name)
	if err != nil {
		return err
	}
	return s.c.inTx(ctx, func(tx Tx) error {
		group, err := tx.Groups().GetByName(ctx, name)
		if err != nil {
			return fmt.Errorf("group %s: %w", name, err)
		}
		members, err := tx.Groups().Members(ctx, group.ID)
		if err != nil {
			return fmt.Errorf("members of group %s: %w", name, err)
		}
		if err := tx.Groups().Delete(ctx, group.ID); err != nil {
			return fmt.Errorf("delete group %s: %w", name, err)
		}
		if members == nil {
			members = []SatelliteID{}
		}
		return tx.Events().Append(ctx, Event{
			Kind:      EventGroupDeleted,
			Aggregate: Aggregate{Type: AggregateGroup, ID: int64(group.ID)},
			Payload:   GroupDeletedPayload{Name: group.Name, Members: members, StateRef: stateRef},
		})
	})
}

func (s *GroupService) Get(ctx context.Context, name string) (Group, error) {
	group, err := s.c.read().Groups().GetByName(ctx, name)
	if err != nil {
		return Group{}, fmt.Errorf("group %s: %w", name, err)
	}
	return group, nil
}

func (s *GroupService) List(ctx context.Context) ([]Group, error) {
	return s.c.read().Groups().List(ctx)
}

func (s *GroupService) Members(ctx context.Context, name string) ([]Satellite, error) {
	read := s.c.read()
	group, err := read.Groups().GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("group %s: %w", name, err)
	}
	ids, err := read.Groups().Members(ctx, group.ID)
	if err != nil {
		return nil, fmt.Errorf("members of group %s: %w", name, err)
	}
	out := make([]Satellite, 0, len(ids))
	for _, id := range ids {
		sat, err := read.Satellites().Get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("member %d of group %s: %w", id, name, err)
		}
		out = append(out, sat)
	}
	return out, nil
}

// AddSatellite and RemoveSatellite change the membership and append
// AssignmentChanged; the satellite reconciler recomputes the robot's
// projects from all of the satellite's groups and republishes its state,
// a state without groups after the last one (D10).
func (s *GroupService) AddSatellite(ctx context.Context, cmd MembershipCmd) error {
	return s.membership(ctx, cmd, func(tx Tx, group GroupID, sat SatelliteID) error {
		return tx.Groups().AddMember(ctx, group, sat)
	})
}

func (s *GroupService) RemoveSatellite(ctx context.Context, cmd MembershipCmd) error {
	return s.membership(ctx, cmd, func(tx Tx, group GroupID, sat SatelliteID) error {
		return tx.Groups().RemoveMember(ctx, group, sat)
	})
}

func (s *GroupService) membership(ctx context.Context, cmd MembershipCmd, change func(Tx, GroupID, SatelliteID) error) error {
	return s.c.inTx(ctx, func(tx Tx) error {
		sat, err := tx.Satellites().GetByName(ctx, cmd.Satellite)
		if err != nil {
			return fmt.Errorf("satellite %s: %w", cmd.Satellite, err)
		}
		group, err := tx.Groups().GetByName(ctx, cmd.Group)
		if err != nil {
			return fmt.Errorf("group %s: %w", cmd.Group, err)
		}
		if err := change(tx, group.ID, sat.ID); err != nil {
			return fmt.Errorf("membership of %s in %s: %w", cmd.Satellite, cmd.Group, err)
		}
		return tx.Events().Append(ctx, satelliteEvent(EventAssignmentChanged, sat))
	})
}

// HasMember reports whether the satellite is in the group.
func (s *GroupService) HasMember(ctx context.Context, cmd MembershipCmd) (bool, error) {
	read := s.c.read()
	sat, err := read.Satellites().GetByName(ctx, cmd.Satellite)
	if err != nil {
		return false, fmt.Errorf("satellite %s: %w", cmd.Satellite, err)
	}
	groups, err := read.Satellites().Groups(ctx, sat.ID)
	if err != nil {
		return false, fmt.Errorf("groups of satellite %s: %w", cmd.Satellite, err)
	}
	return slices.ContainsFunc(groups, func(g Group) bool { return g.Name == cmd.Group }), nil
}
