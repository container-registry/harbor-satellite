package fleet

import (
	"context"
	"time"
)

type RegistryEventType string

const RegistryArtifactPushed RegistryEventType = "ArtifactPushed"

type RegistryEvent struct {
	Type                             RegistryEventType
	Project, Repository, Tag, Digest string
	OccurredAt                       time.Time
}

// RegistryEventSource is the Harbor 3 extension API's feed (plan item 9.5);
// no adapter and no consumer until that API exists.
type RegistryEventSource interface {
	Subscribe(ctx context.Context, handle func(ctx context.Context, event RegistryEvent) error) error
}
