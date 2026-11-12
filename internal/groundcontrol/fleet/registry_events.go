package fleet

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

type RegistryEventType string

const RegistryArtifactPushed RegistryEventType = "ArtifactPushed"

type RegistryEvent struct {
	Type                             RegistryEventType
	Project, Repository, Tag, Digest string
	OccurredAt                       time.Time
}

// RegistryEventSource is the Harbor 3 extension API's feed (plan item 9.5).
// async.RegistryFollower consumes it; no adapter exists until Harbor 3
// publishes that API (docs/guides/harbor3-extension.md).
type RegistryEventSource interface {
	Subscribe(ctx context.Context, handle func(ctx context.Context, event RegistryEvent) error) error
}

// FollowPush applies a pushed artifact to every group that lists its
// repository with the pushed tag under another digest: the digest moves to
// the pushed one and the group's GroupChanged republishes it, so groups
// follow tags (plan item 9.5). It is driven by the registry, not by an
// operator, so it is not policy-checked. It returns the updated groups.
func (s *GroupService) FollowPush(ctx context.Context, event RegistryEvent) ([]string, error) {
	if event.Type != RegistryArtifactPushed || event.Tag == "" || event.Digest == "" {
		return nil, nil
	}
	repo := event.Repository
	if event.Project != "" && !strings.HasPrefix(repo, event.Project+"/") {
		repo = event.Project + "/" + repo
	}
	var updated []string
	err := s.c.inTx(ctx, func(tx Tx) error {
		groups, err := tx.Groups().List(ctx)
		if err != nil {
			return fmt.Errorf("list groups: %w", err)
		}
		for _, g := range groups {
			changed := false
			for i, a := range g.Artifacts {
				if a.Deleted || a.Repository != repo || a.Digest == event.Digest || !slices.Contains(a.Tags, event.Tag) {
					continue
				}
				g.Artifacts[i].Digest = event.Digest
				changed = true
			}
			if !changed {
				continue
			}
			g.Projects = ArtifactProjects(g.Artifacts)
			stored, err := tx.Groups().Upsert(ctx, g)
			if err != nil {
				return fmt.Errorf("store group %s: %w", g.Name, err)
			}
			if err := tx.Events().Append(ctx, groupEvent(EventGroupChanged, stored)); err != nil {
				return err
			}
			updated = append(updated, g.Name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}
