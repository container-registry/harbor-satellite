package store

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
)

type recordingStore struct {
	replicate func(source Store, artifacts []Artifact) error
	calls     int
	sources   []Store
	batches   [][]Artifact
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

func (r *recordingStore) Replicate(_ context.Context, source Store, artifacts []Artifact) error {
	r.calls++
	r.sources = append(r.sources, source)
	r.batches = append(r.batches, append([]Artifact(nil), artifacts...))
	if r.replicate == nil {
		return nil
	}
	return r.replicate(source, artifacts)
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
	require.Equal(t, []Store{peerA, peerA, peerB}, local.sources)
	require.Equal(t, [][]Artifact{{appA}, {appB}, {appB}}, local.batches)
	require.Equal(t, 0, harbor.calls)
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

func TestPeerFillingStoreRequiresLocalStore(t *testing.T) {
	_, err := NewPeerFillingStore(nil, nil, PeerFillOptions{})
	require.ErrorContains(t, err, "local store")
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
