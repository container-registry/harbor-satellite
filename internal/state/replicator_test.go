package state

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestNewBasicReplicator(t *testing.T) {
	tests := []struct {
		name           string
		sourceUsername string
		sourcePassword string
		sourceRegistry string
		remoteURL      string
		remoteUsername string
		remotePassword string
		useUnsecure    bool
	}{
		{
			name:           "all fields provided",
			sourceUsername: "sourceUser",
			sourcePassword: "sourcePass",
			sourceRegistry: "source.registry.com",
			remoteURL:      "remote.registry.com",
			remoteUsername: "remoteUser",
			remotePassword: "remotePass",
			useUnsecure:    false,
		},
		{
			name:           "use unsecure connection",
			sourceUsername: "user",
			sourcePassword: "pass",
			sourceRegistry: "registry.com",
			remoteURL:      "remote.com",
			remoteUsername: "remote",
			remotePassword: "pass",
			useUnsecure:    true,
		},
		{
			name:           "empty credentials",
			sourceUsername: "",
			sourcePassword: "",
			sourceRegistry: "registry.com",
			remoteURL:      "remote.com",
			remoteUsername: "",
			remotePassword: "",
			useUnsecure:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			replicator := NewBasicReplicator(
				tt.sourceUsername,
				tt.sourcePassword,
				tt.sourceRegistry,
				tt.remoteURL,
				tt.remoteUsername,
				tt.remotePassword,
				tt.useUnsecure,
			)

			require.NotNil(t, replicator)
			basicReplicator, ok := replicator.(*BasicReplicator)
			require.True(t, ok)

			require.Equal(t, tt.sourceUsername, basicReplicator.sourceUsername)
			require.Equal(t, tt.sourcePassword, basicReplicator.sourcePassword)
			require.Equal(t, tt.sourceRegistry, basicReplicator.sourceRegistry)
			require.Equal(t, tt.remoteURL, basicReplicator.remoteRegistryURL)
			require.Equal(t, tt.remoteUsername, basicReplicator.remoteUsername)
			require.Equal(t, tt.remotePassword, basicReplicator.remotePassword)
			require.Equal(t, tt.useUnsecure, basicReplicator.useUnsecure)
		})
	}
}

func TestNewBasicReplicatorWithTLS(t *testing.T) {
	tests := []struct {
		name           string
		sourceUsername string
		sourcePassword string
		sourceRegistry string
		remoteURL      string
		remoteUsername string
		remotePassword string
		useUnsecure    bool
		tlsCfg         config.TLSConfig
	}{
		{
			name:           "with TLS config",
			sourceUsername: "user",
			sourcePassword: "pass",
			sourceRegistry: "registry.com",
			remoteURL:      "remote.com",
			remoteUsername: "remote",
			remotePassword: "pass",
			useUnsecure:    false,
			tlsCfg: config.TLSConfig{
				CertFile:   "/path/to/cert.pem",
				KeyFile:    "/path/to/key.pem",
				CAFile:     "/path/to/ca.pem",
				SkipVerify: false,
			},
		},
		{
			name:           "with skip verify",
			sourceUsername: "user",
			sourcePassword: "pass",
			sourceRegistry: "registry.com",
			remoteURL:      "remote.com",
			remoteUsername: "remote",
			remotePassword: "pass",
			useUnsecure:    false,
			tlsCfg: config.TLSConfig{
				SkipVerify: true,
			},
		},
		{
			name:           "empty TLS config",
			sourceUsername: "user",
			sourcePassword: "pass",
			sourceRegistry: "registry.com",
			remoteURL:      "remote.com",
			remoteUsername: "remote",
			remotePassword: "pass",
			useUnsecure:    true,
			tlsCfg:         config.TLSConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			replicator := NewBasicReplicatorWithTLS(
				tt.sourceUsername,
				tt.sourcePassword,
				tt.sourceRegistry,
				tt.remoteURL,
				tt.remoteUsername,
				tt.remotePassword,
				tt.useUnsecure,
				tt.tlsCfg,
			)

			require.NotNil(t, replicator)
			basicReplicator, ok := replicator.(*BasicReplicator)
			require.True(t, ok)

			require.Equal(t, tt.tlsCfg, basicReplicator.tlsCfg)
		})
	}
}

func TestEntity_GetMethods(t *testing.T) {
	entity := Entity{
		Name:       "nginx",
		Repository: "library",
		Tag:        "latest",
		Digest:     "sha256:abc123",
	}

	require.Equal(t, "nginx", entity.GetName())
	require.Equal(t, "library", entity.GetRepository())
	require.Equal(t, "latest", entity.GetTag())
}

func TestBasicReplicator_Replicate_EmptyList(t *testing.T) {
	replicator := NewBasicReplicator(
		"user",
		"pass",
		"source.registry.com",
		"remote.registry.com",
		"remote",
		"pass",
		true,
	)

	ctx := context.Background()
	err := replicator.Replicate(ctx, []Entity{})

	// Should succeed with empty list
	require.NoError(t, err)
}

func TestBasicReplicator_DeleteReplicationEntity_EmptyList(t *testing.T) {
	replicator := NewBasicReplicator(
		"user",
		"pass",
		"source.registry.com",
		"remote.registry.com",
		"remote",
		"pass",
		true,
	)

	ctx := context.Background()
	err := replicator.DeleteReplicationEntity(ctx, []Entity{})

	// Should succeed with empty list
	require.NoError(t, err)
}

func TestBasicReplicator_Replicate_InvalidRegistry(t *testing.T) {
	replicator := NewBasicReplicator(
		"user",
		"pass",
		"invalid://source.registry.com",
		"invalid://remote.registry.com",
		"remote",
		"pass",
		false,
	)

	ctx := context.Background()
	entities := []Entity{
		{
			Name:       "nginx",
			Repository: "library",
			Tag:        "latest",
			Digest:     "sha256:abc123",
		},
	}

	err := replicator.Replicate(ctx, entities)

	// Should fail due to invalid registry URL
	require.Error(t, err)
}

func TestBasicReplicator_DeleteReplicationEntity_InvalidRegistry(t *testing.T) {
	replicator := NewBasicReplicator(
		"user",
		"pass",
		"source.registry.com",
		"invalid://remote.registry.com",
		"remote",
		"pass",
		false,
	)

	ctx := context.Background()
	entities := []Entity{
		{
			Name:       "nginx",
			Repository: "library",
			Tag:        "latest",
			Digest:     "sha256:abc123",
		},
	}

	err := replicator.DeleteReplicationEntity(ctx, entities)

	// Should fail due to invalid registry URL
	require.Error(t, err)
}

func TestBasicReplicator_buildTLSTransport(t *testing.T) {
	tests := []struct {
		name          string
		tlsCfg        config.TLSConfig
		expectNil     bool
		expectError   bool
		errorContains string
	}{
		{
			name: "no TLS config returns nil",
			tlsCfg: config.TLSConfig{
				CertFile: "",
				CAFile:   "",
			},
			expectNil:   true,
			expectError: false,
		},
		{
			name: "invalid cert file path",
			tlsCfg: config.TLSConfig{
				CertFile: "/nonexistent/cert.pem",
				KeyFile:  "/nonexistent/key.pem",
			},
			expectNil:     false,
			expectError:   true,
			errorContains: "load TLS config",
		},
		{
			name: "CA file only",
			tlsCfg: config.TLSConfig{
				CAFile: "/nonexistent/ca.pem",
			},
			expectNil:     false,
			expectError:   true,
			errorContains: "load TLS config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			replicator := &BasicReplicator{
				tlsCfg: tt.tlsCfg,
			}

			transport, err := replicator.buildTLSTransport()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					require.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				if tt.expectNil {
					require.Nil(t, transport)
				} else {
					require.NotNil(t, transport)
				}
			}
		})
	}
}

func TestBasicReplicator_Replicate_ContextCancellation(t *testing.T) {
	replicator := NewBasicReplicator(
		"user",
		"pass",
		"source.registry.com",
		"remote.registry.com",
		"remote",
		"pass",
		true,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	entities := []Entity{
		{
			Name:       "nginx",
			Repository: "library",
			Tag:        "latest",
			Digest:     "sha256:abc123",
		},
	}

	err := replicator.Replicate(ctx, entities)

	// Should fail due to cancelled context
	require.Error(t, err)
}

func TestBasicReplicator_DeleteReplicationEntity_ContextCancellation(t *testing.T) {
	replicator := NewBasicReplicator(
		"user",
		"pass",
		"source.registry.com",
		"remote.registry.com",
		"remote",
		"pass",
		true,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	entities := []Entity{
		{
			Name:       "nginx",
			Repository: "library",
			Tag:        "latest",
			Digest:     "sha256:abc123",
		},
	}

	err := replicator.DeleteReplicationEntity(ctx, entities)

	// Should fail due to cancelled context
	require.Error(t, err)
}

func TestEntity_MultipleEntities(t *testing.T) {
	entities := []Entity{
		{
			Name:       "nginx",
			Repository: "library",
			Tag:        "latest",
			Digest:     "sha256:abc123",
		},
		{
			Name:       "redis",
			Repository: "library",
			Tag:        "7",
			Digest:     "sha256:def456",
		},
		{
			Name:       "postgres",
			Repository: "library",
			Tag:        "15",
			Digest:     "sha256:ghi789",
		},
	}

	require.Len(t, entities, 3)
	require.Equal(t, "nginx", entities[0].GetName())
	require.Equal(t, "redis", entities[1].GetName())
	require.Equal(t, "postgres", entities[2].GetName())
}

func TestBasicReplicator_Replicate_MultipleEntities(t *testing.T) {
	replicator := NewBasicReplicator(
		"user",
		"pass",
		"source.registry.com",
		"remote.registry.com",
		"remote",
		"pass",
		true,
	)

	ctx := context.Background()
	entities := []Entity{
		{
			Name:       "nginx",
			Repository: "library",
			Tag:        "latest",
			Digest:     "sha256:abc123",
		},
		{
			Name:       "redis",
			Repository: "library",
			Tag:        "7",
			Digest:     "sha256:def456",
		},
	}

	err := replicator.Replicate(ctx, entities)

	// Should fail because registries don't exist, but the function should iterate through all entities
	require.Error(t, err)
}

func TestBasicReplicator_TLSConfigValidation(t *testing.T) {
	tlsCfg := config.TLSConfig{
		CertFile:   "/path/to/cert.pem",
		KeyFile:    "/path/to/key.pem",
		CAFile:     "/path/to/ca.pem",
		SkipVerify: false,
	}

	replicator := NewBasicReplicatorWithTLS(
		"user",
		"pass",
		"source.registry.com",
		"remote.registry.com",
		"remote",
		"pass",
		false,
		tlsCfg,
	)

	basicReplicator, ok := replicator.(*BasicReplicator)
	require.True(t, ok)

	// Verify TLS config is stored correctly
	require.Equal(t, "/path/to/cert.pem", basicReplicator.tlsCfg.CertFile)
	require.Equal(t, "/path/to/key.pem", basicReplicator.tlsCfg.KeyFile)
	require.Equal(t, "/path/to/ca.pem", basicReplicator.tlsCfg.CAFile)
	require.False(t, basicReplicator.tlsCfg.SkipVerify)
}