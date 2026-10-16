package policybundle_test

import (
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/policybundle"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

func TestBundle_DigestIsOrderIndependent(t *testing.T) {
	pending.Plan(t, "7.5", "Bundle.Digest is a stub")
	allow := []string{"*", "*", "*", "*", "allow"}
	deny := []string{"*", "library/blocked", "*", "*", "deny"}
	a := policybundle.Bundle{ModelVersion: policybundle.ModelVersion, Policies: [][]string{allow, deny}}
	b := policybundle.Bundle{ModelVersion: policybundle.ModelVersion, Policies: [][]string{deny, allow}}

	require.True(t, strings.HasPrefix(a.Digest(), "sha256:"), "digest %q", a.Digest())
	require.Len(t, a.Digest(), len("sha256:")+64)
	require.Equal(t, a.Digest(), b.Digest(), "line order does not matter")
	onlyAllow := policybundle.Bundle{ModelVersion: policybundle.ModelVersion, Policies: [][]string{allow}}
	require.NotEqual(t, a.Digest(), onlyAllow.Digest())
	otherModel := policybundle.Bundle{ModelVersion: "edge-admission/v2", Policies: a.Policies}
	require.NotEqual(t, a.Digest(), otherModel.Digest(), "the model version is part of the digest")
}

func TestParseMode(t *testing.T) {
	pending.Plan(t, "7.5", "ParseMode is a stub")
	for in, want := range map[string]policybundle.Mode{
		"":        policybundle.ModeOff,
		"off":     policybundle.ModeOff,
		"audit":   policybundle.ModeAudit,
		"enforce": policybundle.ModeEnforce,
	} {
		got, err := policybundle.ParseMode(in)
		require.NoError(t, err, "%q", in)
		require.Equal(t, want, got, "%q", in)
	}
	for _, in := range []string{"block", "Enforce", " enforce"} {
		_, err := policybundle.ParseMode(in)
		require.Error(t, err, "%q", in)
	}
}
