package server

import (
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// PolicyRoute maps one /api route of the generated router, by method and
// path template, to the resource type and action the route check asks the
// policy about (plan item 7.2). Every /api route has exactly one entry
// (TestPolicyRoutes_CoverGeneratedRouter), so a new /api route needs one.
type PolicyRoute struct {
	Method, Path string
	Type         fleet.ResourceType
	Action       fleet.Action
}

var PolicyRoutes = []PolicyRoute{
	{http.MethodGet, "/api/configs", fleet.ResourceConfig, fleet.ActionList},
	{http.MethodPost, "/api/configs", fleet.ResourceConfig, fleet.ActionCreate},
	{http.MethodPost, "/api/configs/satellite", fleet.ResourceSatellite, fleet.ActionAssignConfig},
	{http.MethodDelete, "/api/configs/{config}", fleet.ResourceConfig, fleet.ActionDelete},
	{http.MethodGet, "/api/configs/{config}", fleet.ResourceConfig, fleet.ActionRead},
	{http.MethodPatch, "/api/configs/{config}", fleet.ResourceConfig, fleet.ActionUpdate},
	{http.MethodGet, "/api/groups", fleet.ResourceGroup, fleet.ActionList},
	{http.MethodDelete, "/api/groups/satellite", fleet.ResourceGroup, fleet.ActionRemoveMember},
	{http.MethodPost, "/api/groups/satellite", fleet.ResourceGroup, fleet.ActionAddMember},
	{http.MethodPost, "/api/groups/sync", fleet.ResourceGroup, fleet.ActionSync},
	{http.MethodDelete, "/api/groups/{group}", fleet.ResourceGroup, fleet.ActionDelete},
	{http.MethodGet, "/api/groups/{group}", fleet.ResourceGroup, fleet.ActionRead},
	{http.MethodGet, "/api/groups/{group}/satellites", fleet.ResourceGroup, fleet.ActionRead},
	{http.MethodPost, "/api/logout", fleet.ResourceSession, fleet.ActionLogout},
	{http.MethodGet, "/api/ops/outbox", fleet.ResourceOps, fleet.ActionRead},
	{http.MethodGet, "/api/policies", fleet.ResourcePolicy, fleet.ActionList},
	{http.MethodPost, "/api/policies", fleet.ResourcePolicy, fleet.ActionCreate},
	{http.MethodDelete, "/api/policies/{id}", fleet.ResourcePolicy, fleet.ActionDelete},
	{http.MethodGet, "/api/role-bindings", fleet.ResourcePolicy, fleet.ActionList},
	{http.MethodPost, "/api/role-bindings", fleet.ResourcePolicy, fleet.ActionCreate},
	{http.MethodDelete, "/api/role-bindings/{id}", fleet.ResourcePolicy, fleet.ActionDelete},
	{http.MethodGet, "/api/satellites", fleet.ResourceSatellite, fleet.ActionList},
	{http.MethodPost, "/api/satellites", fleet.ResourceSatellite, fleet.ActionRegister},
	{http.MethodGet, "/api/satellites/active", fleet.ResourceSatellite, fleet.ActionList},
	{http.MethodPost, "/api/satellites/register", fleet.ResourceSPIRE, fleet.ActionRegisterSPIFFE},
	{http.MethodGet, "/api/satellites/stale", fleet.ResourceSatellite, fleet.ActionList},
	{http.MethodDelete, "/api/satellites/{satellite}", fleet.ResourceSatellite, fleet.ActionDelete},
	{http.MethodGet, "/api/satellites/{satellite}", fleet.ResourceSatellite, fleet.ActionRead},
	{http.MethodGet, "/api/satellites/{satellite}/images", fleet.ResourceSatellite, fleet.ActionRead},
	{http.MethodGet, "/api/satellites/{satellite}/lifecycle", fleet.ResourceSatellite, fleet.ActionRead},
	{http.MethodPost, "/api/satellites/{satellite}/revoke", fleet.ResourceSatellite, fleet.ActionRevoke},
	{http.MethodGet, "/api/satellites/{satellite}/status", fleet.ResourceSatellite, fleet.ActionRead},
	{http.MethodPost, "/api/satellites/{satellite}/unrevoke", fleet.ResourceSatellite, fleet.ActionUnrevoke},
	{http.MethodGet, "/api/spire/agents", fleet.ResourceSPIRE, fleet.ActionList},
	{http.MethodGet, "/api/spire/status", fleet.ResourceSPIRE, fleet.ActionRead},
	{http.MethodGet, "/api/users", fleet.ResourceUser, fleet.ActionList},
	{http.MethodPost, "/api/users", fleet.ResourceUser, fleet.ActionCreate},
	{http.MethodPatch, "/api/users/password", fleet.ResourceUser, fleet.ActionChangePassword},
	{http.MethodDelete, "/api/users/{username}", fleet.ResourceUser, fleet.ActionDelete},
	{http.MethodGet, "/api/users/{username}", fleet.ResourceUser, fleet.ActionRead},
	{http.MethodPatch, "/api/users/{username}/password", fleet.ResourceUser, fleet.ActionSetPassword},
	{http.MethodGet, "/api/webhooks", fleet.ResourceWebhook, fleet.ActionList},
	{http.MethodPost, "/api/webhooks", fleet.ResourceWebhook, fleet.ActionCreate},
	{http.MethodDelete, "/api/webhooks/{id}", fleet.ResourceWebhook, fleet.ActionDelete},
}
