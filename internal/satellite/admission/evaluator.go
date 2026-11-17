package admission

import (
	"context"
	"fmt"
	"sync"

	casbinlib "github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
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

// maxCachedDecisions bounds the cache; a full cache starts over, which
// costs one evaluation per request until it fills again.
const maxCachedDecisions = 10000

// Evaluator is a Casbin enforcer over one bundle and policybundle.Model.
// It is safe for concurrent use.
type Evaluator struct {
	enforcer *casbinlib.SyncedEnforcer
	digest   string
	mu       sync.Mutex
	cache    map[Request]Decision
}

// NewEvaluator fails for a bundle whose ModelVersion is not
// policybundle.ModelVersion or whose lines do not load.
func NewEvaluator(bundle policybundle.Bundle) (*Evaluator, error) {
	if bundle.ModelVersion != policybundle.ModelVersion {
		return nil, fmt.Errorf("admission bundle model %q: this satellite runs %q", bundle.ModelVersion, policybundle.ModelVersion)
	}
	m, err := model.NewModelFromString(policybundle.Model)
	if err != nil {
		return nil, fmt.Errorf("admission model: %w", err)
	}
	e, err := casbinlib.NewSyncedEnforcer(m)
	if err != nil {
		return nil, fmt.Errorf("admission enforcer: %w", err)
	}
	for i, line := range bundle.Policies {
		if len(line) != 5 {
			return nil, fmt.Errorf("admission bundle line %d has %d fields, want 5", i+1, len(line))
		}
		if line[4] != "allow" && line[4] != "deny" {
			return nil, fmt.Errorf("admission bundle line %d: effect %q, want allow or deny", i+1, line[4])
		}
	}
	if len(bundle.Policies) > 0 {
		if _, err := e.AddPolicies(bundle.Policies); err != nil {
			return nil, fmt.Errorf("load admission bundle: %w", err)
		}
	}
	return &Evaluator{enforcer: e, digest: bundle.Digest(), cache: map[Request]Decision{}}, nil
}

// Decide caches per (bundle digest, subject, action, repository, reference),
// because a by-tag request has no digest before the store lookup.
func (e *Evaluator) Decide(_ context.Context, req Request) (Decision, error) {
	e.mu.Lock()
	d, ok := e.cache[req]
	e.mu.Unlock()
	if ok {
		d.Cached = true
		return d, nil
	}
	allowed, explain, err := e.enforcer.EnforceEx(req.Subject, req.Repository, req.Reference, req.Action)
	if err != nil {
		return Decision{}, fmt.Errorf("evaluate admission: %w", err)
	}
	d = Decision{Allowed: allowed, Rule: explain}
	e.mu.Lock()
	if len(e.cache) >= maxCachedDecisions {
		e.cache = map[Request]Decision{}
	}
	e.cache[req] = d
	e.mu.Unlock()
	return d, nil
}
