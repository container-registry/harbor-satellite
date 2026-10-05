package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistryReferenceUsesEndpointRepository(t *testing.T) {
	options := RegistryOptions{Endpoint: "https://registry.example.com/", Repository: "/teams/platform/images/"}
	artifact := Artifact{Name: "httpd", Repository: "ignored/source/project", Tag: "2.4-trixie"}
	require.Equal(t, "registry.example.com/teams/platform/images/httpd:2.4-trixie", options.reference(artifact, artifact.Tag))
}

func TestArtifactSourceIdentifierAcceptsFullDigestReference(t *testing.T) {
	artifact := Artifact{Name: "busybox", Tag: "latest", Digest: "busybox@sha256:92b1d1cae5f235812184415e63d9b24464116c58d3ba3c460b1eb0247f0f46e3"}
	require.Equal(t, "sha256:92b1d1cae5f235812184415e63d9b24464116c58d3ba3c460b1eb0247f0f46e3", artifact.sourceIdentifier())
}

func TestArtifactValidationAndEmptyRepositoryReference(t *testing.T) {
	require.ErrorContains(t, (Artifact{Tag: "latest"}).validate(), "name")
	require.ErrorContains(t, (Artifact{Name: "alpine"}).validate(), "tag or digest")
	require.NoError(t, (Artifact{Name: "alpine", Tag: "latest"}).validate())

	options := RegistryOptions{Endpoint: "https://registry.example.com"}
	require.Equal(t, "registry.example.com/alpine:latest", options.reference(Artifact{Name: "alpine"}, "latest"))
}

func TestStoreConstructorsValidateConfiguration(t *testing.T) {
	_, err := NewOCIStore("")
	require.ErrorContains(t, err, "root")
	_, err = NewRegistryStore(RegistryOptions{})
	require.ErrorContains(t, err, "endpoint")
}
