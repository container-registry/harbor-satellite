package state

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestCanExecute(t *testing.T) {
	process := &FetchAndReplicateStateProcess{name: "test"}

	tests := []struct {
		name              string
		satelliteStateURL string
		remoteURL         string
		srcURL            string
		srcUsername       string
		srcPassword       string
		expectCanExecute  bool
		expectMsgContains string
	}{
		{
			name:              "all fields present",
			satelliteStateURL: "https://registry.example.com/state",
			remoteURL:         "https://remote.example.com",
			srcURL:            "https://source.example.com",
			srcUsername:       "user",
			srcPassword:       "pass",
			expectCanExecute:  true,
			expectMsgContains: "all conditions fulfilled",
		},
		{
			name:              "missing satelliteStateURL",
			satelliteStateURL: "",
			remoteURL:         "https://remote.example.com",
			srcURL:            "https://source.example.com",
			srcUsername:       "user",
			srcPassword:       "pass",
			expectCanExecute:  false,
			expectMsgContains: "satelliteState is empty",
		},
		{
			name:              "missing remoteURL",
			satelliteStateURL: "https://registry.example.com/state",
			remoteURL:         "",
			srcURL:            "https://source.example.com",
			srcUsername:       "user",
			srcPassword:       "pass",
			expectCanExecute:  false,
			expectMsgContains: "remote registry URL is empty",
		},
		{
			name:              "missing srcUsername",
			satelliteStateURL: "https://registry.example.com/state",
			remoteURL:         "https://remote.example.com",
			srcURL:            "https://source.example.com",
			srcUsername:       "",
			srcPassword:       "pass",
			expectCanExecute:  false,
			expectMsgContains: "username is empty",
		},
		{
			name:              "missing srcPassword",
			satelliteStateURL: "https://registry.example.com/state",
			remoteURL:         "https://remote.example.com",
			srcURL:            "https://source.example.com",
			srcUsername:       "user",
			srcPassword:       "",
			expectCanExecute:  false,
			expectMsgContains: "password is empty",
		},
		{
			name:              "missing srcURL",
			satelliteStateURL: "https://registry.example.com/state",
			remoteURL:         "https://remote.example.com",
			srcURL:            "",
			srcUsername:       "user",
			srcPassword:       "pass",
			expectCanExecute:  false,
			expectMsgContains: "source registry is empty",
		},
		{
			name:              "multiple missing fields",
			satelliteStateURL: "",
			remoteURL:         "",
			srcURL:            "",
			srcUsername:       "",
			srcPassword:       "",
			expectCanExecute:  false,
			expectMsgContains: "missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canExecute, reason := process.CanExecute(
				tt.satelliteStateURL,
				tt.remoteURL,
				tt.srcURL,
				tt.srcUsername,
				tt.srcPassword,
			)

			require.Equal(t, tt.expectCanExecute, canExecute)
			require.Contains(t, reason, tt.expectMsgContains)
		})
	}
}

func TestGetChanges(t *testing.T) {
	process := &FetchAndReplicateStateProcess{name: "test"}
	logger := zerolog.Nop()

	t.Run("new state with no old entities replicates all", func(t *testing.T) {
		newState := &State{
			Registry: "registry.example.com",
			Artifacts: []Artifact{
				{Name: "image1", Repository: "repo1", Tags: []string{"v1"}, Digest: "sha256:abc"},
				{Name: "image2", Repository: "repo2", Tags: []string{"v2"}, Digest: "sha256:def"},
			},
		}

		toDelete, toReplicate, _ := process.GetChanges(newState, &logger, nil)

		require.Empty(t, toDelete)
		require.Len(t, toReplicate, 2)
	})

	t.Run("same entities no changes", func(t *testing.T) {
		oldEntities := []Entity{
			{Name: "image1", Repository: "repo1", Tag: "v1", Digest: "sha256:abc"},
		}

		newState := &State{
			Registry: "registry.example.com",
			Artifacts: []Artifact{
				{Name: "image1", Repository: "repo1", Tags: []string{"v1"}, Digest: "sha256:abc"},
			},
		}

		toDelete, toReplicate, _ := process.GetChanges(newState, &logger, oldEntities)

		require.Empty(t, toDelete)
		require.Empty(t, toReplicate)
	})

	t.Run("added entity replicates new", func(t *testing.T) {
		oldEntities := []Entity{
			{Name: "image1", Repository: "repo1", Tag: "v1", Digest: "sha256:abc"},
		}

		newState := &State{
			Registry: "registry.example.com",
			Artifacts: []Artifact{
				{Name: "image1", Repository: "repo1", Tags: []string{"v1"}, Digest: "sha256:abc"},
				{Name: "image2", Repository: "repo2", Tags: []string{"v2"}, Digest: "sha256:def"},
			},
		}

		toDelete, toReplicate, _ := process.GetChanges(newState, &logger, oldEntities)

		require.Empty(t, toDelete)
		require.Len(t, toReplicate, 1)
		require.Equal(t, "image2", toReplicate[0].Name)
	})

	t.Run("removed entity deletes old", func(t *testing.T) {
		oldEntities := []Entity{
			{Name: "image1", Repository: "repo1", Tag: "v1", Digest: "sha256:abc"},
			{Name: "image2", Repository: "repo2", Tag: "v2", Digest: "sha256:def"},
		}

		newState := &State{
			Registry: "registry.example.com",
			Artifacts: []Artifact{
				{Name: "image1", Repository: "repo1", Tags: []string{"v1"}, Digest: "sha256:abc"},
			},
		}

		toDelete, toReplicate, _ := process.GetChanges(newState, &logger, oldEntities)

		require.Len(t, toDelete, 1)
		require.Equal(t, "image2", toDelete[0].Name)
		require.Empty(t, toReplicate)
	})

	t.Run("changed digest deletes old and replicates new", func(t *testing.T) {
		oldEntities := []Entity{
			{Name: "image1", Repository: "repo1", Tag: "v1", Digest: "sha256:old"},
		}

		newState := &State{
			Registry: "registry.example.com",
			Artifacts: []Artifact{
				{Name: "image1", Repository: "repo1", Tags: []string{"v1"}, Digest: "sha256:new"},
			},
		}

		toDelete, toReplicate, _ := process.GetChanges(newState, &logger, oldEntities)

		require.Len(t, toDelete, 1)
		require.Equal(t, "sha256:old", toDelete[0].Digest)
		require.Len(t, toReplicate, 1)
		require.Equal(t, "sha256:new", toReplicate[0].Digest)
	})
}

func TestContains(t *testing.T) {
	t.Run("item in slice returns true", func(t *testing.T) {
		slice := []string{"a", "b", "c"}
		require.True(t, contains(slice, "b"))
	})

	t.Run("item not in slice returns false", func(t *testing.T) {
		slice := []string{"a", "b", "c"}
		require.False(t, contains(slice, "d"))
	})

	t.Run("empty slice returns false", func(t *testing.T) {
		var slice []string
		require.False(t, contains(slice, "a"))
	})
}

func TestFetchEntitiesFromState(t *testing.T) {
	t.Run("multiple artifacts with multiple tags", func(t *testing.T) {
		state := &State{
			Registry: "registry.example.com",
			Artifacts: []Artifact{
				{Name: "image1", Repository: "repo1", Tags: []string{"v1", "latest"}, Digest: "sha256:abc"},
				{Name: "image2", Repository: "repo2", Tags: []string{"v2"}, Digest: "sha256:def"},
			},
		}

		entities := FetchEntitiesFromState(state)

		require.Len(t, entities, 3)
		require.Equal(t, "v1", entities[0].Tag)
		require.Equal(t, "latest", entities[1].Tag)
		require.Equal(t, "v2", entities[2].Tag)
	})

	t.Run("empty state returns empty entities", func(t *testing.T) {
		state := &State{
			Registry:  "registry.example.com",
			Artifacts: []Artifact{},
		}

		entities := FetchEntitiesFromState(state)
		require.Empty(t, entities)
	})
}

func TestRemoveNullTagArtifacts(t *testing.T) {
	process := &FetchAndReplicateStateProcess{name: "test"}

	t.Run("filters out null tags", func(t *testing.T) {
		state := &State{
			Registry: "registry.example.com",
			Artifacts: []Artifact{
				{Name: "image1", Repository: "repo1", Tags: []string{"v1"}, Digest: "sha256:abc"},
				{Name: "image2", Repository: "repo2", Tags: nil, Digest: "sha256:def"},
				{Name: "image3", Repository: "repo3", Tags: []string{}, Digest: "sha256:ghi"},
			},
		}

		result := process.RemoveNullTagArtifacts(state)

		require.Len(t, result.GetArtifacts(), 1)
		require.Equal(t, "image1", result.GetArtifacts()[0].GetName())
	})

	t.Run("keeps artifacts with valid tags", func(t *testing.T) {
		state := &State{
			Registry: "registry.example.com",
			Artifacts: []Artifact{
				{Name: "image1", Repository: "repo1", Tags: []string{"v1", "latest"}, Digest: "sha256:abc"},
			},
		}

		result := process.RemoveNullTagArtifacts(state)

		require.Len(t, result.GetArtifacts(), 1)
	})
}
