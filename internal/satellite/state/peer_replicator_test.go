package state

import (
	"context"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/assert"
)

// mockReplicator is a simple mock that implements the Replicator interface.
type mockReplicator struct {
	replicateCalled       bool
	deleteCalled          bool
	entitiesReplicated    []Entity
	entitiesDeleted       []Entity
	replicateReturnError  error
	deleteReturnError     error
}

func (m *mockReplicator) Replicate(ctx context.Context, entities []Entity) error {
	m.replicateCalled = true
	m.entitiesReplicated = entities
	return m.replicateReturnError
}

func (m *mockReplicator) DeleteReplicationEntity(ctx context.Context, entities []Entity) error {
	m.deleteCalled = true
	m.entitiesDeleted = entities
	return m.deleteReturnError
}

func TestNewPeerAwareReplicator(t *testing.T) {
	t.Run("panics on nil fallback", func(t *testing.T) {
		assert.PanicsWithValue(t, "fallback replicator cannot be nil", func() {
			NewPeerAwareReplicator(
				config.P2PConfig{},
				nil,
				"local-url",
				"user",
				"pass",
				false,
			)
		})
	})

	t.Run("successfully creates replicator", func(t *testing.T) {
		mockFallback := &mockReplicator{}
		p2pCfg := config.P2PConfig{
			Enabled:            true,
			Peers:              []string{"http://10.0.0.1"},
			AcquisitionTimeout: 30 * time.Second,
		}

		r := NewPeerAwareReplicator(
			p2pCfg,
			mockFallback,
			"local-url",
			"user",
			"pass",
			false,
		)

		assert.NotNil(t, r)
		assert.Equal(t, p2pCfg, r.p2pCfg)
		assert.Equal(t, mockFallback, r.fallback)
		assert.Equal(t, "local-url", r.localURL)
		assert.Equal(t, "user", r.localUsername)
		assert.Equal(t, "pass", r.localPassword)
		assert.False(t, r.useUnsecure)
	})
}

func TestPeerAwareReplicator_Delegation(t *testing.T) {
	t.Run("delegates Replicate to fallback", func(t *testing.T) {
		mockFallback := &mockReplicator{}
		r := NewPeerAwareReplicator(config.P2PConfig{}, mockFallback, "", "", "", false)

		entities := []Entity{
			{Name: "image1", Repository: "library", Tag: "latest"},
		}

		err := r.Replicate(context.Background(), entities)
		assert.NoError(t, err)
		assert.True(t, mockFallback.replicateCalled)
		assert.Equal(t, entities, mockFallback.entitiesReplicated)
	})

	t.Run("delegates DeleteReplicationEntity to fallback", func(t *testing.T) {
		mockFallback := &mockReplicator{}
		r := NewPeerAwareReplicator(config.P2PConfig{}, mockFallback, "", "", "", false)

		entities := []Entity{
			{Name: "image1", Repository: "library", Tag: "latest"},
		}

		err := r.DeleteReplicationEntity(context.Background(), entities)
		assert.NoError(t, err)
		assert.True(t, mockFallback.deleteCalled)
		assert.Equal(t, entities, mockFallback.entitiesDeleted)
	})
}
