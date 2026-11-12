package auth

import (
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
	"github.com/stretchr/testify/require"
)

func TestLoginOIDCFlag_DefaultOff(t *testing.T) {
	cmd := NewLoginCommand(nil)
	flag := cmd.Flags().Lookup("oidc")
	require.NotNil(t, flag)
	require.Equal(t, "false", flag.DefValue)

	cmd.SetArgs([]string{"--oidc"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	require.ErrorIs(t, cmd.Execute(), notimpl.ErrNotImplemented, "the device flow is plan item 8.1 and needs no --username")

	plain := NewLoginCommand(nil)
	plain.SetArgs([]string{})
	plain.SilenceUsage, plain.SilenceErrors = true, true
	require.ErrorContains(t, plain.Execute(), "--username", "a password login still needs one")
}
