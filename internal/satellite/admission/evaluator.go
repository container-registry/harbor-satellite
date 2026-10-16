package admission

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
	"github.com/container-registry/harbor-satellite/internal/shared/policybundle"
)

// Request is one proxy operation. Subject is the client identity when the
// proxy has one and "" otherwise; Action is the proxy.Operation name
// ("pull_manifest"). Reference is the tag or digest of a manifest
// operation and "*" for blob operations, which are checked on the
// repository only: a blob request names no tag, and a blob shared by an
// allowed and a denied manifest must not decide differently per path.
type Request struct{ Subject, Repository, Reference, Action string }

type Decision struct {
	Allowed, Cached bool
	// Rule is the deciding policy line, for the log.
	Rule []string
}

// Evaluator is a Casbin enforcer over one bundle and policybundle.Model.
// It is safe for concurrent use.
type Evaluator struct{}

// NewEvaluator fails for a bundle whose ModelVersion is not
// policybundle.ModelVersion or whose lines do not load.
func NewEvaluator(bundle policybundle.Bundle) (*Evaluator, error) {
	return nil, notimpl.Err("7.5 admission evaluator")
}

// Decide caches per (bundle digest, subject, action, repository, reference),
// because a by-tag request has no digest before the store lookup.
func (e *Evaluator) Decide(ctx context.Context, req Request) (Decision, error) {
	return Decision{}, notimpl.Err("7.5 admission decision")
}
