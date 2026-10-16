---
status: proposed
date: 2026-10-16
deciders: [Harbor Satellite Development Team]
informed: [Harbor Satellite Developers]
---
# Policy engine: Casbin for Ground Control and edge admission

## Context

Plan item 7 (policy) found these limits at `508b0d6`:

* **Authorization is hard-coded.** `requiresSystemAdmin` in `internal/groundcontrol/server/routes.go` lists the routes only `system_admin` may call; every other `/api` route is open to any authenticated user. There is no way to scope an admin to a group, a satellite or a config.
* **Config documents are stored unchecked.** Ground Control accepts any `app_config`; the satellite validates it only after it has replicated the config state.
* **ADR-0009 proposed CEL** for edge admission in the proxy. Nothing of it is implemented.
* Gate G1 chose Casbin for both Ground Control's scoped administration and edge admission.

## Decision Drivers

* No user gains a permission they do not have today when the engine is switched on.
* A scoped admin cannot reach users, SPIRE, operations or the policy API, and cannot reach a group outside its scope through a satellite or a config.
* Every replica sees a policy change within a bounded time, and a broken change never removes the last good policy.
* One policy vocabulary for Ground Control and the edge.

## Decision

1. **Two layers (7.1, 7.2).** The `/api` route middleware asks `fleet.Policy.AuthorizeType` by role (coarse). The services ask `Authorize` on the concrete resource after loading its parent scopes with `Groups().GroupsOfSatellites`, one statement that also filters lists. Subjects (`user:`, `role:`, `oidc:`) and parents are resolved per request; the built-in roles come from `users.role` and are never stored.
2. **Stored policy (7.3).** Rules and additional role bindings live in `policy_rules` and `policy_role_bindings` (migration 020). Every write bumps `policy_version` and appends a log-only `PolicyChanged` in the same transaction. Replicas poll the version every `POLICY_POLL_INTERVAL` and reload one REPEATABLE READ snapshot, version first.
3. **No escalation.** Scoped rules exist only for satellite, group and config types; `PolicyService` rejects anything else. SPIFFE registration is checked as `register_spiffe` on `spire`, because it creates SPIRE entries and join tokens, so no scoped rule reaches it. The matcher receives the subjects and the resource's objects as string lists, never joined, so no character in a user, group or OIDC name can forge an entry. `SatelliteService.Register` and `ConfigService.Assign` need `add_member` on every group they touch. Tests prove that a group admin gets 403 on the policy API, users, SPIRE and ops.
4. **Equivalence first.** The embedded baseline reproduces `requiresSystemAdmin` on every generated `/api` route (`TestPolicyEquivalence_RouteByRole`); `POLICY_ENGINE=builtin` stays the default until that holds.
5. **Config validation (7.4).** `fleet.ConfigValidator`, implemented by `internal/groundcontrol/satconfig` over `pkg/config`, runs before a config is stored: an invalid document is 422 with the warnings, warnings of a valid one are returned.
6. **Edge admission (7.5).** The bundle (`internal/shared/policybundle`) travels in `app_config.admission`, inside the config state artifact, with modes `off`, `audit` and `enforce`. `internal/satellite/admission` evaluates it in the proxy and caches decisions per (bundle digest, subject, operation, repository, reference), because a by-tag request has no digest before the store lookup. A blob request is checked on its repository: any allow there admits it, tag-scoped ones included, and only a deny on every reference denies it, so an image whose tag is allowed can be pulled whole.
7. **Failure.** Ground Control fails closed at startup when it cannot load the policy, and keeps the last good policy when a reload fails.

## Consequences

* Good: scoped administration without new parameters on the services; one vocabulary at the edge and in Ground Control.
* Bad: a policy change takes up to the poll interval to reach other replicas.
* Bad: Casbin and its transitive dependencies (`govaluate`) enter the module.
* Neutral: supersedes ADR-0009's CEL section.

## Implementation Status

| Part | Status |
|---|---|
| ADR | proposed |
| Migration `020_policy_rules.sql` and its queries | done, SQL contract tests |
| `adapters/casbin` (model, baseline, `RouteMatcher`), `fleet.PolicyService`, `PolicyRepo` and its postgres adapter, scoped checks in the services | skeleton, pending tests |
| `server.PolicyRoutes`, `PolicyMiddleware` (used only with `POLICY_ENGINE=casbin` and a policy), policy and role-binding routes | routes in the spec and `PolicyRoutes`; handlers answer 501 |
| `fleet.ConfigValidator`, `satconfig.Validator`, 422 on config writes | skeleton, pending tests |
| `policybundle`, `internal/satellite/admission`, `app_config.admission` | skeleton, pending tests; `cmd/satellite` does not use it |

## Validation

* The pending tests of plan item 7 pass without their markers, among them `TestPolicyEquivalence_RouteByRole` and `TestScopedAdmin_CannotReachNonScopedRoutes`.
* `TestPolicyRoutes_CoverGeneratedRouter` stays green: every `/api` route has exactly one `PolicyRoutes` entry.
