package admission_test

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/admission"
	"github.com/container-registry/harbor-satellite/internal/shared/policybundle"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

var (
	allowAll     = []string{"*", "*", "*", "*", "allow"}
	blockRepo    = []string{"*", "library/blocked", "*", "*", "deny"}
	blockOldTags = []string{"*", "library/alpine", "3.1*", "*", "deny"}
)

func bundle(lines ...[]string) policybundle.Bundle {
	return policybundle.Bundle{ModelVersion: policybundle.ModelVersion, Policies: lines}
}

func evaluator(t *testing.T, b policybundle.Bundle) *admission.Evaluator {
	t.Helper()
	e, err := admission.NewEvaluator(b)
	require.NoError(t, err)
	return e
}

func decide(t *testing.T, e *admission.Evaluator, req admission.Request) admission.Decision {
	t.Helper()
	d, err := e.Decide(context.Background(), req)
	require.NoError(t, err)
	return d
}

func pull(repo, ref string) admission.Request {
	return admission.Request{Repository: repo, Reference: ref, Action: "pull_manifest"}
}

func TestEvaluator_DeniesBlockedRepository(t *testing.T) {
	pending.Plan(t, "7.5", "the admission evaluator is a stub")
	e := evaluator(t, bundle(allowAll, blockRepo))

	denied := decide(t, e, pull("library/blocked", "latest"))
	require.False(t, denied.Allowed)
	require.Equal(t, blockRepo, denied.Rule)
	require.True(t, decide(t, e, pull("library/alpine", "latest")).Allowed)
	require.False(t, decide(t, evaluator(t, bundle()), pull("library/alpine", "latest")).Allowed, "a bundle without an allow admits nothing")
}

func TestEvaluator_CachesPerBundleAndReference(t *testing.T) {
	pending.Plan(t, "7.5", "the admission evaluator is a stub")
	e := evaluator(t, bundle(allowAll, blockOldTags))

	first := decide(t, e, pull("library/alpine", "3.19"))
	require.False(t, first.Allowed)
	require.False(t, first.Cached)
	again := decide(t, e, pull("library/alpine", "3.19"))
	require.False(t, again.Allowed)
	require.True(t, again.Cached)
	require.False(t, decide(t, e, pull("library/alpine", "3.20")).Cached, "another reference")
	head := pull("library/alpine", "3.19")
	head.Action = "check_manifest"
	require.False(t, decide(t, e, head).Cached, "another operation")
	as := pull("library/alpine", "3.19")
	as.Subject = "spiffe://harbor-satellite.local/client/a"
	require.False(t, decide(t, e, as).Cached, "another subject")

	other := evaluator(t, bundle(allowAll))
	require.False(t, decide(t, other, pull("library/alpine", "3.19")).Cached, "another bundle")
}

func TestEvaluator_TagOnlyAllowAdmitsBlobs(t *testing.T) {
	pending.Plan(t, "7.5", "the admission evaluator is a stub")
	e := evaluator(t, bundle([]string{"*", "library/app", "v1*", "*", "allow"}))

	require.True(t, decide(t, e, pull("library/app", "v1.2")).Allowed)
	require.False(t, decide(t, e, pull("library/app", "v2.0")).Allowed)
	require.True(t, decide(t, e, admission.Request{Repository: "library/app", Reference: "*", Action: "pull_blob"}).Allowed, "the image can be pulled whole")
}

func TestEvaluator_RejectsUnknownModelVersion(t *testing.T) {
	pending.Plan(t, "7.5", "the admission evaluator is a stub")
	_, err := admission.NewEvaluator(policybundle.Bundle{ModelVersion: "edge-admission/v99", Policies: [][]string{allowAll}})
	require.ErrorContains(t, err, "edge-admission/v99")
	_, err = admission.NewEvaluator(bundle([]string{"*", "*"}))
	require.Error(t, err, "a line of the wrong length")

	_, err = admission.NewEvaluator(bundle(allowAll))
	require.NoError(t, err)
}

// A blob request carries no tag: its Reference is "*", so tag rules do not
// apply to it and repository rules do.
func TestEvaluator_BlobChecksRepositoryOnly(t *testing.T) {
	pending.Plan(t, "7.5", "the admission evaluator is a stub")
	e := evaluator(t, bundle(allowAll, blockRepo, blockOldTags))
	blob := func(repo string) admission.Request {
		return admission.Request{Repository: repo, Reference: "*", Action: "pull_blob"}
	}

	require.True(t, decide(t, e, blob("library/alpine")).Allowed, "the tag rule does not apply")
	require.False(t, decide(t, e, blob("library/blocked")).Allowed, "the repository rule does")
	require.False(t, decide(t, e, pull("library/alpine", "3.19")).Allowed)
}
