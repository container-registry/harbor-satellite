package common

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestDecodeManifestJSONFileRejectsNullRoot(t *testing.T) {
	command := &cobra.Command{}
	command.SetIn(strings.NewReader("null\n"))

	_, err := DecodeManifestJSONFile(command, "-")
	require.ErrorContains(t, err, "manifest must be an object")
}

func TestDecodeManifestJSONFileAcceptsEmptyObject(t *testing.T) {
	command := &cobra.Command{}
	command.SetIn(strings.NewReader("{}\n"))

	manifest, err := DecodeManifestJSONFile(command, "-")
	require.NoError(t, err)
	require.JSONEq(t, "{}", string(manifest))
}
