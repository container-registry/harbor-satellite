// Package casbin implements fleet.Policy with Casbin (plan item 7,
// ADR-0017): the embedded model and baseline reproduce today's route
// rules, and the stored rules and bindings of migration 020 add scoped
// administration on top.
package casbin

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	casbinlib "github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
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
type Policy struct {
	repo     fleet.PolicyRepo
	interval time.Duration
	current  atomic.Pointer[snapshot]
	// reload serialises Reload, so two pollers never build twice.
	reload sync.Mutex
}

type snapshot struct {
	version  int64
	enforcer *casbinlib.SyncedEnforcer
}

var _ fleet.Policy = (*Policy)(nil)

// New loads Baseline plus repo.Snapshot; serve fails closed when it
// cannot. Run reloads every pollInterval.
func New(ctx context.Context, repo fleet.PolicyRepo, pollInterval time.Duration) (*Policy, error) {
	if repo == nil || pollInterval <= 0 {
		return nil, fmt.Errorf("%w: the casbin policy needs a policy repository and a positive poll interval", fleet.ErrInvalid)
	}
	p := &Policy{repo: repo, interval: pollInterval}
	snap, err := repo.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("load policy: %w", err)
	}
	built, err := build(snap)
	if err != nil {
		return nil, err
	}
	p.current.Store(built)
	return p, nil
}

// build compiles the model with Baseline and the snapshot's rules (p) and
// bindings (g). A line outside the vocabulary fails the whole build.
func build(snap fleet.PolicySnapshot) (*snapshot, error) {
	m, err := model.NewModelFromString(Model)
	if err != nil {
		return nil, fmt.Errorf("policy model: %w", err)
	}
	e, err := casbinlib.NewSyncedEnforcer(m)
	if err != nil {
		return nil, fmt.Errorf("policy enforcer: %w", err)
	}
	var rules [][]string
	for line := range strings.Lines(Baseline) {
		fields := strings.Split(strings.TrimSpace(line), ",")
		if len(fields) != 6 || fields[0] != "p" {
			continue
		}
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		rules = append(rules, fields[1:])
	}
	for _, r := range snap.Rules {
		if r.Effect != fleet.EffectAllow && r.Effect != fleet.EffectDeny {
			return nil, fmt.Errorf("%w: policy rule %d has effect %q", fleet.ErrInvalid, r.ID, r.Effect)
		}
		rules = append(rules, []string{r.Subject, r.Scope, r.Type, r.Action, r.Effect})
	}
	if len(rules) > 0 {
		if _, err := e.AddPolicies(rules); err != nil {
			return nil, fmt.Errorf("load policy rules: %w", err)
		}
	}
	if len(snap.Bindings) > 0 {
		bindings := make([][]string, 0, len(snap.Bindings))
		for _, b := range snap.Bindings {
			bindings = append(bindings, []string{b.Principal, b.Role})
		}
		if _, err := e.AddGroupingPolicies(bindings); err != nil {
			return nil, fmt.Errorf("load role bindings: %w", err)
		}
	}
	e.AddFunction("listHas", listHas)
	e.AddFunction("anyLinked", anyLinked(e.GetRoleManager()))
	return &snapshot{version: snap.Version, enforcer: e}, nil
}

func decision(allowed bool, explain []string, err error) (fleet.Decision, error) {
	if err != nil {
		return fleet.Decision{}, fmt.Errorf("enforce policy: %w", err)
	}
	return fleet.Decision{Allowed: allowed, Rule: strings.Join(explain, ", ")}, nil
}

// Authorize answers with the deciding line in Decision.Rule.
func (p *Policy) Authorize(_ context.Context, principal fleet.Principal, action fleet.Action, resource fleet.Resource) (fleet.Decision, error) {
	e := p.current.Load().enforcer
	return decision(e.EnforceEx(requestSubjects(principal), requestObjects(resource), string(resource.Type), string(action)))
}

func (p *Policy) AuthorizeType(_ context.Context, principal fleet.Principal, action fleet.Action, typ fleet.ResourceType) (fleet.Decision, error) {
	e := p.current.Load().enforcer
	return decision(e.EnforceExWithMatcher(RouteMatcher, requestSubjects(principal), []string{}, string(typ), string(action)))
}

// Reload reads repo.Version and, when it moved, the snapshot, and swaps
// the enforcer. On failure the last good enforcer stays.
func (p *Policy) Reload(ctx context.Context) error {
	p.reload.Lock()
	defer p.reload.Unlock()
	version, err := p.repo.Version(ctx)
	if err != nil {
		return fmt.Errorf("read policy version: %w", err)
	}
	if version == p.current.Load().version {
		return nil
	}
	snap, err := p.repo.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("load policy: %w", err)
	}
	built, err := build(snap)
	if err != nil {
		return err
	}
	p.current.Store(built)
	return nil
}

// Name and Run make the poller a job of every serve process, with or
// without a worker.
func (p *Policy) Name() string { return "policy-poller" }

// Run reloads every pollInterval until ctx ends; a failed reload is logged
// and retried at the next tick.
func (p *Policy) Run(ctx context.Context) error {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := p.Reload(ctx); err != nil && ctx.Err() == nil {
				log.Printf("policy poller: %v", err)
			}
		}
	}
}
