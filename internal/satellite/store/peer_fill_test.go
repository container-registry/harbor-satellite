package store

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
)

type recordingStore struct {
	mu           sync.Mutex
	replicate    func(source Store, artifacts []Artifact) error
	replicateCtx func(ctx context.Context, source Store, artifacts []Artifact) error
	calls        int
	sources      []Store
	batches      [][]Artifact
}

func (r *recordingStore) Pull(context.Context, Artifact, PullResource) (ocispec.Descriptor, error) {
	return ocispec.Descriptor{}, errors.New("unexpected pull")
}

func (r *recordingStore) Fetch(context.Context, Artifact, ocispec.Descriptor, PullResource, http.Header) (*http.Response, error) {
	return nil, errors.New("unexpected fetch")
}

func (r *recordingStore) Delete(context.Context, []Artifact) error {
	return errors.New("unexpected delete")
}

func (r *recordingStore) Replicate(ctx context.Context, source Store, artifacts []Artifact) error {
	batch := append([]Artifact(nil), artifacts...)
	r.mu.Lock()
	r.calls++
	r.sources = append(r.sources, source)
	r.batches = append(r.batches, batch)
	replicate := r.replicate
	replicateCtx := r.replicateCtx
	r.mu.Unlock()
	if replicateCtx != nil {
		return replicateCtx(ctx, source, artifacts)
	}
	if replicate == nil {
		return nil
	}
	return replicate(source, artifacts)
}

func (r *recordingStore) snapshot() (int, []Store, [][]Artifact) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sources := append([]Store(nil), r.sources...)
	batches := make([][]Artifact, len(r.batches))
	for i, batch := range r.batches {
		batches[i] = append([]Artifact(nil), batch...)
	}
	return r.calls, sources, batches
}

func TestPeerFillingStoreCopiesFromFirstCompletePeer(t *testing.T) {
	peer := &recordingStore{}
	harbor := &recordingStore{}
	local := &recordingStore{}
	filling, err := NewPeerFillingStore(local, func() []Store { return []Store{peer} }, PeerFillOptions{Timeout: time.Second})
	require.NoError(t, err)

	err = filling.Replicate(context.Background(), harbor, []Artifact{{Name: "demo/app", Tag: "latest"}})

	require.NoError(t, err)
	require.Equal(t, 1, local.calls)
	require.Equal(t, peer, local.sources[0])
	require.Equal(t, 0, harbor.calls)
}

func TestPeerFillingStoreRetriesUnreachablePeerOnceThenUsesHarbor(t *testing.T) {
	peerErr := errors.New("connection refused")
	peer := &recordingStore{}
	harbor := &recordingStore{}
	local := &recordingStore{replicate: func(source Store, _ []Artifact) error {
		if source == peer {
			return peerErr
		}
		return nil
	}}
	filling, err := NewPeerFillingStore(local, func() []Store { return []Store{peer} }, PeerFillOptions{Timeout: time.Second, Retries: 1})
	require.NoError(t, err)

	err = filling.Replicate(context.Background(), harbor, []Artifact{{Name: "demo/app", Tag: "latest"}})

	require.NoError(t, err)
	require.Equal(t, 3, local.calls)
	require.Equal(t, []Store{peer, peer, harbor}, local.sources)
}

func TestPeerFillingStoreCopiesEachImageFromThePeerThatHasIt(t *testing.T) {
	appA := Artifact{Name: "demo/app-a", Tag: "latest"}
	appB := Artifact{Name: "demo/app-b", Tag: "latest"}
	peerA := &recordingStore{}
	peerB := &recordingStore{}
	harbor := &recordingStore{}
	local := &recordingStore{replicate: func(source Store, artifacts []Artifact) error {
		if len(artifacts) != 1 {
			return errors.New("expected one artifact")
		}
		if source == peerA && artifacts[0].Name == appB.Name {
			return errors.New("peer A does not have app-b")
		}
		if source == peerB && artifacts[0].Name == appA.Name {
			return errors.New("peer B does not have app-a")
		}
		return nil
	}}
	filling, err := NewPeerFillingStore(local, func() []Store { return []Store{peerA, peerB} }, PeerFillOptions{Timeout: time.Second})
	require.NoError(t, err)

	err = filling.Replicate(context.Background(), harbor, []Artifact{appA, appB})

	require.NoError(t, err)
	harborCalls, _, _ := harbor.snapshot()
	require.Equal(t, 0, harborCalls)
	_, sources, batches := local.snapshot()
	require.True(t, copiedFrom(sources, batches, peerA, appA.Name))
	require.True(t, copiedFrom(sources, batches, peerB, appB.Name))
}

func TestPeerFillingStoreUsesHarborOnlyForTheImagePeersLack(t *testing.T) {
	appA := Artifact{Name: "demo/app-a", Tag: "latest"}
	appB := Artifact{Name: "demo/app-b", Tag: "latest"}
	peer := &recordingStore{}
	harbor := &recordingStore{}
	local := &recordingStore{replicate: func(source Store, artifacts []Artifact) error {
		if source == peer && len(artifacts) == 1 && artifacts[0].Name == appB.Name {
			return errors.New("peer does not have app-b")
		}
		return nil
	}}
	filling, err := NewPeerFillingStore(local, func() []Store { return []Store{peer} }, PeerFillOptions{Timeout: time.Second})
	require.NoError(t, err)

	err = filling.Replicate(context.Background(), harbor, []Artifact{appA, appB})

	require.NoError(t, err)
	require.Equal(t, []Store{peer, peer, harbor}, local.sources)
	require.Equal(t, [][]Artifact{{appA}, {appB}, {appB}}, local.batches)
}

func TestPeerFillingStoreSkipsPeersWhenNoneAreEligible(t *testing.T) {
	harbor := &recordingStore{}
	local := &recordingStore{}
	filling, err := NewPeerFillingStore(local, func() []Store { return nil }, PeerFillOptions{Timeout: time.Second, Retries: 1})
	require.NoError(t, err)

	err = filling.Replicate(context.Background(), harbor, []Artifact{{Name: "demo/app", Tag: "latest"}})

	require.NoError(t, err)
	require.Equal(t, []Store{harbor}, local.sources)
}

func TestPeerFillingStoreKeepsRacingAfterAPeerMiss(t *testing.T) {
	failFast := &recordingStore{}
	slow := &recordingStore{}
	harbor := &recordingStore{}
	local := &recordingStore{
		replicateCtx: func(ctx context.Context, source Store, _ []Artifact) error {
			if source == failFast {
				return errors.New("peer miss")
			}
			timer := time.NewTimer(40 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
	filling, err := NewPeerFillingStore(local, func() []Store { return []Store{failFast, slow} }, PeerFillOptions{Timeout: time.Second})
	require.NoError(t, err)

	err = filling.Replicate(context.Background(), harbor, []Artifact{{Name: "demo/app", Tag: "latest"}})

	require.NoError(t, err)
	harborCalls, _, _ := harbor.snapshot()
	require.Equal(t, 0, harborCalls)
}

func TestPeerFillingStoreCancelsSlowerPeersAfterCompleteGraph(t *testing.T) {
	blocker := &recordingStore{}
	fast := &recordingStore{}
	harbor := &recordingStore{}
	local := &recordingStore{
		replicateCtx: func(ctx context.Context, source Store, _ []Artifact) error {
			if source != blocker {
				return nil
			}
			<-ctx.Done()
			return ctx.Err()
		},
	}
	filling, err := NewPeerFillingStore(local, func() []Store { return []Store{blocker, fast} }, PeerFillOptions{Timeout: 5 * time.Second})
	require.NoError(t, err)

	start := time.Now()
	err = filling.Replicate(context.Background(), harbor, []Artifact{{Name: "demo/app", Tag: "latest"}})

	require.NoError(t, err)
	require.Less(t, time.Since(start), time.Second)
	harborCalls, _, _ := harbor.snapshot()
	require.Equal(t, 0, harborCalls)
}

func TestPeerFillingStoreLimitsInFlightPeers(t *testing.T) {
	peerA := &recordingStore{}
	peerB := &recordingStore{}
	harbor := &recordingStore{}
	var inflight atomic.Int32
	var maxInflight atomic.Int32
	var attempts atomic.Int32
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	local := &recordingStore{
		replicateCtx: func(_ context.Context, _ Store, _ []Artifact) error {
			current := inflight.Add(1)
			for {
				seen := maxInflight.Load()
				if current <= seen || maxInflight.CompareAndSwap(seen, current) {
					break
				}
			}
			entered <- struct{}{}
			<-release
			inflight.Add(-1)
			if attempts.Add(1) == 1 {
				return errors.New("first peer missed")
			}
			return nil
		},
	}
	filling, err := NewPeerFillingStore(local, func() []Store { return []Store{peerA, peerB} }, PeerFillOptions{Timeout: time.Second, Retries: 0, Concurrency: 1})
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		done <- filling.Replicate(context.Background(), harbor, []Artifact{{Name: "demo/app", Tag: "latest"}})
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first peer did not start")
	}
	select {
	case <-entered:
		t.Fatal("second peer started before the first finished")
	case <-time.After(50 * time.Millisecond):
	}
	release <- struct{}{}

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("second peer did not start after the first finished")
	}
	require.Equal(t, int32(1), maxInflight.Load())
	release <- struct{}{}

	require.NoError(t, <-done)
	harborCalls, _, _ := harbor.snapshot()
	require.Equal(t, 0, harborCalls)
	require.Equal(t, int32(1), maxInflight.Load())
}

func TestPeerFillingStoreCopiesRegistryDestinationOnePeerAtATime(t *testing.T) {
	registry, err := NewRegistryStore(RegistryOptions{Endpoint: "127.0.0.1:9", PlainHTTP: true})
	require.NoError(t, err)
	filling, err := NewPeerFillingStore(registry, func() []Store { return nil }, PeerFillOptions{Concurrency: 4})
	require.NoError(t, err)
	require.Equal(t, 1, filling.peerConcurrency())

	filling, err = NewPeerFillingStore(&recordingStore{}, func() []Store { return nil }, PeerFillOptions{Concurrency: 4})
	require.NoError(t, err)
	require.Equal(t, 4, filling.peerConcurrency())
}

func copiedFrom(sources []Store, batches [][]Artifact, peer Store, name string) bool {
	for i, source := range sources {
		if source == peer && len(batches[i]) == 1 && batches[i][0].Name == name {
			return true
		}
	}
	return false
}

func TestPeerFillingStoreRequiresLocalStore(t *testing.T) {
	_, err := NewPeerFillingStore(nil, nil, PeerFillOptions{})
	require.ErrorContains(t, err, "local store")
}

func TestPeerFillingStoreBoundsHarborCopyWhenTimeoutSet(t *testing.T) {
	harbor := &recordingStore{}
	peer := &recordingStore{}
	local := &recordingStore{
		replicateCtx: func(ctx context.Context, source Store, _ []Artifact) error {
			if source != harbor {
				return errors.New("peer miss")
			}
			<-ctx.Done()
			return ctx.Err()
		},
	}
	filling, err := NewPeerFillingStore(local, func() []Store { return []Store{peer} }, PeerFillOptions{Timeout: time.Millisecond})
	require.NoError(t, err)

	start := time.Now()
	err = filling.Replicate(WithHarborTimeout(context.Background(), 40*time.Millisecond), harbor, []Artifact{{Name: "demo/app", Tag: "latest"}})
	require.Error(t, err)
	require.Less(t, time.Since(start), time.Second)
}

func TestHarborTimeoutFromEmptyContext(t *testing.T) {
	require.Equal(t, time.Duration(0), HarborTimeoutFrom(context.Background()))
	require.Equal(t, 30*time.Second, HarborTimeoutFrom(WithHarborTimeout(context.Background(), 30*time.Second)))
	require.Equal(t, context.Background(), WithHarborTimeout(context.Background(), 0))
}

func TestLabelStoreNamesThePeer(t *testing.T) {
	require.Nil(t, LabelStore(nil, "http://peer"))
	labeled := LabelStore(&recordingStore{}, "http://127.0.0.1:8585")
	require.Equal(t, "http://127.0.0.1:8585", storeLabel(labeled))
}

type graphStore struct {
	*recordingStore
}

func (g *graphStore) targetFor(Artifact) (oras.Target, error) {
	return nil, errors.New("opened graph target")
}

func TestLabelStoreKeepsGraphTransfer(t *testing.T) {
	labeled := LabelStore(&graphStore{recordingStore: &recordingStore{}}, "http://127.0.0.1:8585")
	target, ok := labeled.(storeTarget)
	require.True(t, ok)
	_, err := target.targetFor(Artifact{Name: "demo/test-image", Tag: "latest"})
	require.ErrorContains(t, err, "opened graph target")
}
