package fleettest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/stretchr/testify/require"
)

func TestRegistryEventSource_DeliversInOrder(t *testing.T) {
	boom := errors.New("handler failed")
	src := &fleettest.RegistryEventSource{Events: []fleet.RegistryEvent{
		{Type: fleet.RegistryArtifactPushed, Repository: "library/alpine", Tag: "3.20"},
		{Type: fleet.RegistryArtifactPushed, Repository: "library/busybox", Tag: "1.36"},
	}}
	var seen []string

	require.NoError(t, src.Subscribe(context.Background(), func(_ context.Context, e fleet.RegistryEvent) error {
		seen = append(seen, e.Repository)
		if e.Repository == "library/busybox" {
			return boom
		}
		return nil
	}))

	require.Equal(t, []string{"library/alpine", "library/busybox"}, seen)
	require.Equal(t, []error{nil, boom}, src.Errors)
}
