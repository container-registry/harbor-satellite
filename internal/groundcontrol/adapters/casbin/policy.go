// Package casbin implements fleet.Policy with Casbin (plan item 7,
// ADR-0017): the embedded model and baseline reproduce today's route
// rules, and the stored rules and bindings of migration 020 add scoped
// administration on top.
package casbin

import (
	"context"
	_ "embed"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// Model is the request (subs, objs, typ, act) against policy lines (sub,
// scope, typ, act, eft) model. r.subs and r.objs are string lists
// built per request (functions.go); a rule applies when one of the
// subjects is or has the rule's subject, its scope is "*" or one of the
// objects, and its type and action match or are "*". Any allow and no deny
// admits.
//
//go:embed model.conf
var Model string

// Baseline is today's authorization: system_admin may do everything, admin
// everything except what requiresSystemAdmin (server/routes.go) and the
// user handlers keep for system_admin. TestPolicyEquivalence_RouteByRole
// pins it to the routes.
//
//go:embed baseline.csv
var Baseline string

// RouteMatcher is AuthorizeType's matcher: a rule counts when its scope is
// "*", or when it is a scoped allow on a satellite, group or config type.
// A scoped admin therefore passes the route check of its own types only,
// and a scoped deny cannot close a route for everybody.
const RouteMatcher = `anyLinked(r.subs, p.sub) && (p.scope == "*" || (p.eft == "allow" && (p.typ == "satellite" || p.typ == "group" || p.typ == "config"))) && (p.typ == "*" || r.typ == p.typ) && (p.act == "*" || r.act == p.act)`

// Policy is a Casbin enforcer over Baseline plus the stored policy. It is
// safe for concurrent use; a reload swaps the whole enforcer.
type Policy struct{}

var _ fleet.Policy = (*Policy)(nil)

// New loads Baseline plus repo.Snapshot; serve fails closed when it
// cannot. Run reloads every pollInterval.
func New(ctx context.Context, repo fleet.PolicyRepo, pollInterval time.Duration) (*Policy, error) {
	return nil, notimpl.Err("7.1 casbin policy")
}

// Authorize answers with the deciding line in Decision.Rule.
func (p *Policy) Authorize(ctx context.Context, principal fleet.Principal, action fleet.Action, resource fleet.Resource) (fleet.Decision, error) {
	return fleet.Decision{}, notimpl.Err("7.3 casbin authorize")
}

func (p *Policy) AuthorizeType(ctx context.Context, principal fleet.Principal, action fleet.Action, typ fleet.ResourceType) (fleet.Decision, error) {
	return fleet.Decision{}, notimpl.Err("7.2 casbin authorize route")
}

// Reload reads repo.Version and, when it moved, the snapshot, and swaps
// the enforcer. On failure the last good enforcer stays.
func (p *Policy) Reload(ctx context.Context) error {
	return notimpl.Err("7.3 casbin reload")
}

// Name and Run make the poller a job of every serve process, with or
// without a worker.
func (p *Policy) Name() string { return "policy-poller" }

// Run reloads every pollInterval until ctx ends; a failed reload is logged
// and retried at the next tick.
func (p *Policy) Run(ctx context.Context) error {
	return notimpl.Err("7.3 casbin policy poller")
}
