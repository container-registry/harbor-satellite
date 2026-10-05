package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistryReferenceUsesEndpointRepository(t *testing.T) {
	options := RegistryOptions{
		Endpoint:   "https://registry.example.com/",
		Repository: "/teams/platform/images/",
	}
	artifact := Artifact{
		Name:       "httpd",
		Repository: "ignored/source/project",
		Tag:        "2.4-trixie",
	}

	require.Equal(t, "registry.example.com/teams/platform/images/httpd:2.4-trixie", options.reference(artifact, artifact.Tag))
}

func TestRegistryReferenceAllowsEmptyRepository(t *testing.T) {
	artifact := Artifact{Name: "httpd", Tag: "latest"}

	options := RegistryOptions{Endpoint: "registry.example.com"}
	require.Equal(t, "registry.example.com/httpd:latest", options.reference(artifact, artifact.Tag))
}

func TestArtifactSourceIdentifierAcceptsFullDigestReference(t *testing.T) {
	artifact := Artifact{
		Name:   "busybox",
		Tag:    "latest",
		Digest: "busybox@sha256:92b1d1cae5f235812184415e63d9b24464116c58d3ba3c460b1eb0247f0f46e3",
	}

	require.Equal(t, "sha256:92b1d1cae5f235812184415e63d9b24464116c58d3ba3c460b1eb0247f0f46e3", artifact.sourceIdentifier())
}

func TestArtifactValidateRequiresImageAndIdentifier(t *testing.T) {
	require.Error(t, (Artifact{Tag: "latest"}).validate())
	require.Error(t, (Artifact{Name: "httpd"}).validate())
	require.NoError(t, (Artifact{Name: "httpd", Digest: "sha256:abc"}).validate())
}
