package ready

import (
	"context"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/stretchr/testify/require"
)

func TestGateWaitsUntilTheMatchingDigestIsCommitted(t *testing.T) {
	t.Parallel()
	gate := New()
	image := store.Artifact{Repository: "library", Name: "nginx", Tag: "alpine", Digest: "nginx@sha256:abc"}
	request := store.Artifact{Name: "library/nginx", Tag: "alpine"}
	gate.SetGroup("edge", []store.Artifact{image})

	require.True(t, gate.Watching(request))
	require.True(t, gate.Known(request))
	digest, ok := gate.ExpectedDigest(request)
	require.True(t, ok)
	require.Equal(t, "sha256:abc", digest)

	release := make(chan struct{})
	go func() {
		defer close(release)
		time.Sleep(20 * time.Millisecond)
		gate.Commit(store.Artifact{Repository: "library", Name: "nginx", Tag: "alpine", Digest: "sha256:old"})
		gate.Commit(image)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ready, err := gate.Wait(ctx, request)
	require.NoError(t, err)
	require.True(t, ready)
	<-release
	require.False(t, gate.Watching(request))

	digestRequest := store.Artifact{Name: "library/nginx", Digest: "sha256:abc"}
	ready, err = gate.Wait(ctx, digestRequest)
	require.NoError(t, err)
	require.True(t, ready)
}

func TestGateDropWakesWaiters(t *testing.T) {
	t.Parallel()
	gate := New()
	image := store.Artifact{Name: "team/app", Tag: "latest", Digest: "sha256:abc"}
	gate.SetGroup("edge", []store.Artifact{image})

	done := make(chan bool, 1)
	go func() {
		ready, err := gate.Wait(context.Background(), image)
		done <- err == nil && !ready
	}()
	require.Eventually(t, func() bool {
		return gate.Watching(image)
	}, time.Second, time.Millisecond)
	gate.RemoveGroup("edge")

	select {
	case dropped := <-done:
		require.True(t, dropped)
	case <-time.After(time.Second):
		t.Fatal("drop did not wake the waiter")
	}
	require.False(t, gate.Known(image))
}

func TestGateDigestChangeRestartsTheWait(t *testing.T) {
	t.Parallel()
	gate := New()
	first := store.Artifact{Name: "team/app", Tag: "latest", Digest: "sha256:first"}
	second := store.Artifact{Name: "team/app", Tag: "latest", Digest: "sha256:second"}
	gate.SetGroup("edge", []store.Artifact{first})

	done := make(chan bool, 1)
	go func() {
		ready, err := gate.Wait(context.Background(), first)
		done <- err == nil && ready
	}()
	require.Eventually(t, func() bool { return gate.Watching(first) }, time.Second, time.Millisecond)
	gate.SetGroup("edge", []store.Artifact{second})
	gate.Commit(first)
	select {
	case <-done:
		t.Fatal("commit of the replaced digest released the waiter")
	case <-time.After(30 * time.Millisecond):
	}
	digest, ok := gate.ExpectedDigest(first)
	require.True(t, ok)
	require.Equal(t, "sha256:second", digest)
	gate.Commit(second)
	select {
	case ready := <-done:
		require.True(t, ready)
	case <-time.After(time.Second):
		t.Fatal("commit of the new digest did not release the waiter")
	}
}

func TestGateKeepsASharedReferenceWhenOneGroupDropsIt(t *testing.T) {
	t.Parallel()
	gate := New()
	image := store.Artifact{Name: "team/app", Tag: "latest", Digest: "sha256:abc"}
	gate.SetGroup("east", []store.Artifact{image})
	gate.SetGroup("west", []store.Artifact{image})
	gate.Commit(image)
	gate.RemoveGroup("east")

	require.True(t, gate.Known(image))
	ready, err := gate.Wait(context.Background(), image)
	require.NoError(t, err)
	require.True(t, ready)
}
