package state

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
	"github.com/rs/zerolog"
)

// StateArtifactChanged resolves the state artifact's digest with one HEAD
// and reports whether it differs from lastDigest, so an unchanged satellite
// or group state is not pulled again (plan item 6.1, as the config already
// does). It never pulls the artifact.
func StateArtifactChanged(ctx context.Context, fetcher StateFetcher, lastDigest string, log *zerolog.Logger) (digest string, changed bool, err error) {
	return "", true, notimpl.Err("6.1 state digest shortcut")
}
