package server

import (
	dbmodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	gcmodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/models"
)

// swagger:route GET /ping health ping
//
// Pings the server.
//
// Produces:
// - text/plain
//
// Responses:
//   200: body:string Server is reachable and returns pong.

// swagger:route GET /health health health
//
// Checks database connectivity.
//
// Responses:
//   200: body:APIHealthResponse Database connection is healthy.
//   503: body:APIHealthResponse Database connection is unavailable.

// swagger:route POST /login auth login
//
// Creates a user session.
//
// Responses:
//   200: body:LoginResponse Session token created.
//   400: body:AppError Request body is malformed.
//   401: body:AppError Credentials are missing, invalid, or locked out.
//   429: body:AppError Too many login attempts.
//   500: body:AppError Session could not be created.

// swagger:route POST /api/logout auth logout
//
// Deletes the current user session.
//
// Security:
//   bearerAuth:
//
// Responses:
//   204: description:Session deleted.
//   401: body:AppError Authorization token is missing or invalid.
//   500: body:AppError Session could not be deleted.

// swagger:route GET /api/users users listUsers
//
// Lists non-system users.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:[]UserResponse Users returned.
//   401: body:AppError Authorization token is missing or invalid.
//   500: body:AppError Users could not be listed.

// swagger:route POST /api/users users createUser
//
// Creates an admin user.
//
// Security:
//   bearerAuth:
//
// Responses:
//   201: body:UserResponse User created.
//   400: body:AppError Request body, username, or password policy is invalid.
//   401: body:AppError Authorization token is missing or invalid.
//   403: body:AppError System administrator role is required.
//   409: body:AppError User already exists.
//   500: body:AppError User could not be created.

// swagger:route GET /api/users/{username} users getUser
//
// Gets a user by username.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:UserResponse User returned.
//   401: body:AppError Authorization token is missing or invalid.
//   404: body:AppError User was not found.
//   500: body:AppError User could not be loaded.

// swagger:route DELETE /api/users/{username} users deleteUser
//
// Deletes a user.
//
// Security:
//   bearerAuth:
//
// Responses:
//   204: description:User deleted.
//   400: body:AppError User cannot be deleted by this request.
//   401: body:AppError Authorization token is missing or invalid.
//   403: body:AppError System administrator role is required.
//   404: body:AppError User was not found.
//   500: body:AppError User could not be deleted.

// swagger:route PATCH /api/users/password users changeOwnPassword
//
// Changes the authenticated user's password.
//
// Security:
//   bearerAuth:
//
// Responses:
//   204: description:Password changed and sessions invalidated.
//   400: body:AppError Request body or new password is invalid.
//   401: body:AppError Authorization token is missing, invalid, or current password is incorrect.
//   500: body:AppError Password could not be changed.

// swagger:route PATCH /api/users/{username}/password users changeUserPassword
//
// Resets a user's password.
//
// Security:
//   bearerAuth:
//
// Responses:
//   204: description:Password reset and user sessions invalidated.
//   400: body:AppError Request body or new password is invalid.
//   401: body:AppError Authorization token is missing or invalid.
//   403: body:AppError System administrator role is required.
//   404: body:AppError User was not found.
//   500: body:AppError Password could not be reset.

// swagger:route GET /api/groups groups listGroups
//
// Lists groups.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:[]APIGroup Groups returned.
//   401: body:AppError Authorization token is missing or invalid.
//   500: body:AppError Groups could not be listed.

// swagger:route POST /api/groups/sync groups syncGroup
//
// Synchronizes a group state artifact.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:APIGroup Group synchronized.
//   400: body:AppError Group state artifact payload is invalid.
//   401: body:AppError Authorization token is missing or invalid.
//   502: body:AppError Harbor project lookup or creation failed.
//   500: body:AppError Group could not be synchronized.

// swagger:route GET /api/groups/{group} groups getGroup
//
// Gets a group by name.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:APIGroup Group returned.
//   401: body:AppError Authorization token is missing or invalid.
//   404: body:AppError Group was not found.
//   500: body:AppError Group could not be loaded.

// swagger:route DELETE /api/groups/{group} groups deleteGroup
//
// Deletes a group.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:APIEmptyObject Group deleted.
//   401: body:AppError Authorization token is missing or invalid.
//   403: body:AppError System administrator role is required.
//   404: body:AppError Group was not found.
//   500: body:AppError Group could not be deleted.

// swagger:route GET /api/groups/{group}/satellites groups listGroupSatellites
//
// Lists satellites attached to a group.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:[]APIGroupSatellite Group satellites returned.
//   401: body:AppError Authorization token is missing or invalid.
//   404: body:AppError Group was not found.
//   500: body:AppError Group satellites could not be listed.

// swagger:route POST /api/groups/satellite groups addSatelliteToGroup
//
// Adds a satellite to a group.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:APIMessageResponse Satellite was added to the group or was already a member.
//   400: body:AppError Satellite or group is invalid or not found.
//   401: body:AppError Authorization token is missing or invalid.
//   500: body:AppError Satellite could not be added to the group.

// swagger:route DELETE /api/groups/satellite groups removeSatelliteFromGroup
//
// Removes a satellite from a group.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:APIEmptyObject Satellite removed from the group.
//   400: body:AppError Satellite or group is invalid or not found.
//   401: body:AppError Authorization token is missing or invalid.
//   500: body:AppError Satellite could not be removed from the group.

// swagger:route GET /api/configs configs listConfigs
//
// Lists satellite configurations.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:[]APIDatabaseConfig Configurations returned.
//   401: body:AppError Authorization token is missing or invalid.
//   500: body:AppError Configurations could not be listed.

// swagger:route POST /api/configs configs createConfig
//
// Creates a satellite configuration.
//
// Security:
//   bearerAuth:
//
// Responses:
//   201: description:Configuration created.
//   400: body:AppError Configuration payload or name is invalid.
//   401: body:AppError Authorization token is missing or invalid.
//   409: body:AppError Configuration already exists.
//   500: body:AppError Configuration could not be created.

// swagger:route GET /api/configs/{config} configs getConfig
//
// Gets a satellite configuration.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:APIDatabaseConfig Configuration returned.
//   401: body:AppError Authorization token is missing or invalid.
//   404: body:AppError Configuration was not found.
//   500: body:AppError Configuration could not be loaded.

// swagger:route PATCH /api/configs/{config} configs updateConfig
//
// Applies a merge patch to a satellite configuration.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:APIDatabaseConfig Configuration updated.
//   400: body:AppError Configuration name, body, or merge patch is invalid.
//   401: body:AppError Authorization token is missing or invalid.
//   404: body:AppError Configuration was not found.
//   500: body:AppError Configuration could not be updated.

// swagger:route DELETE /api/configs/{config} configs deleteConfig
//
// Deletes an unused satellite configuration.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:APIEmptyObject Configuration deleted.
//   400: body:AppError Configuration is in use and cannot be deleted.
//   401: body:AppError Authorization token is missing or invalid.
//   500: body:AppError Configuration could not be deleted.

// swagger:route POST /api/configs/satellite configs setSatelliteConfig
//
// Sets the configuration assigned to a satellite.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:APIEmptyObject Satellite configuration assignment updated.
//   400: body:AppError Assignment payload, satellite, or configuration is invalid.
//   401: body:AppError Authorization token is missing or invalid.
//   500: body:AppError Satellite configuration assignment could not be updated.

// swagger:route GET /api/satellites satellites listSatellites
//
// Lists satellites.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:[]APISatellite Satellites returned.
//   401: body:AppError Authorization token is missing or invalid.
//   500: body:AppError Satellites could not be listed.

// swagger:route POST /api/satellites satellites registerSatellite
//
// Registers a token-managed satellite.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:RegisterSatelliteResponse Satellite registered with a zero-touch token.
//   400: body:AppError Registration payload, name, config, or robot account is invalid.
//   401: body:AppError Authorization token is missing or invalid.
//   500: body:AppError Satellite could not be registered.

// swagger:route GET /api/satellites/active satellites listActiveSatellites
//
// Lists active satellites.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:[]APIActiveSatellite Active satellites returned.
//   401: body:AppError Authorization token is missing or invalid.
//   500: body:AppError Active satellites could not be listed.

// swagger:route GET /api/satellites/stale satellites listStaleSatellites
//
// Lists stale satellites.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:[]APIStaleSatellite Stale satellites returned.
//   401: body:AppError Authorization token is missing or invalid.
//   500: body:AppError Stale satellites could not be listed.

// swagger:route GET /api/satellites/{satellite} satellites getSatellite
//
// Gets a satellite by name.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:APISatellite Satellite returned.
//   401: body:AppError Authorization token is missing or invalid.
//   500: body:AppError Satellite could not be loaded.

// swagger:route DELETE /api/satellites/{satellite} satellites deleteSatellite
//
// Deletes a satellite.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:APIEmptyObject Satellite deleted.
//   400: body:AppError Satellite does not exist or cannot be deleted.
//   401: body:AppError Authorization token is missing or invalid.
//   500: body:AppError Satellite could not be deleted.

// swagger:route GET /api/satellites/{satellite}/status satellites getSatelliteStatus
//
// Gets the latest satellite status report.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:APISatelliteStatus Latest satellite status returned.
//   401: body:AppError Authorization token is missing or invalid.
//   404: body:AppError Satellite or status report was not found.
//   500: body:AppError Satellite status could not be loaded.

// swagger:route GET /api/satellites/{satellite}/images satellites getCachedImages
//
// Lists the latest cached images reported by a satellite.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:[]APIDatabaseArtifact Cached images returned.
//   401: body:AppError Authorization token is missing or invalid.
//   404: body:AppError Satellite was not found.
//   500: body:AppError Cached images could not be loaded.

// swagger:route GET /api/spire/status spire getSpireStatus
//
// Gets SPIRE integration status.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:SPIREStatusResponse SPIRE integration status returned.
//   401: body:AppError Authorization token is missing or invalid.
//   403: body:AppError System administrator role is required.

// swagger:route GET /api/spire/agents spire listSpireAgents
//
// Lists attested SPIRE agents.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:AgentListResponse SPIRE agents returned.
//   401: body:AppError Authorization token is missing or invalid.
//   403: body:AppError System administrator role is required.
//   503: body:AppError SPIRE server is not configured.
//   500: body:AppError SPIRE agents could not be listed.

// swagger:route POST /api/satellites/register spire registerSatelliteWithSpiffe
//
// Registers a satellite workload identity with SPIFFE.
//
// Security:
//   bearerAuth:
//
// Responses:
//   200: body:RegisterSatelliteWithSPIFFEResponse SPIFFE workload identity registered.
//   400: body:AppError Registration payload, selectors, or attestation method is invalid.
//   401: body:AppError Authorization token is missing or invalid.
//   403: body:AppError System administrator role is required.
//   404: body:AppError Required parent agent was not found.
//   503: body:AppError SPIRE server is not configured.
//   500: body:AppError SPIFFE registration could not be completed.

// swagger:route GET /satellites/ztr/{token} satellites ztr
//
// Performs token-based zero-touch registration.
//
// Responses:
//   200: body:APIStateConfig Zero-touch registration completed and registry credentials returned.
//   400: body:AppError Registration token is invalid.
//   401: body:AppError Registration token has expired.
//   429: body:AppError Too many zero-touch registration attempts.
//   500: body:AppError Zero-touch registration could not be completed.

// swagger:route GET /satellites/spiffe-ztr satellites spiffeZtr
//
// Performs SPIFFE-based zero-touch registration.
//
// Responses:
//   200: body:APIStateConfig SPIFFE zero-touch registration completed and registry credentials returned.
//   400: body:AppError SPIFFE ID format or satellite identity is invalid.
//   401: body:AppError SPIFFE authentication is missing or invalid.
//   429: body:AppError Too many SPIFFE zero-touch registration attempts.
//   500: body:AppError SPIFFE zero-touch registration could not be completed.

// swagger:route POST /satellites/sync satellites syncSatellite
//
// Reports satellite status.
//
// Responses:
//   200: description:Satellite status report accepted.
//   400: body:AppError Status payload or heartbeat interval is invalid.
//   403: body:AppError Satellite identity is unknown or not authorized.
//   500: body:AppError Satellite status report could not be saved.

// swagger:parameters login
type loginParams struct {
	// User credentials.
	//
	// in: body
	// required: true
	Body loginRequest
}

// swagger:parameters createUser
type createUserParams struct {
	// User creation request.
	//
	// in: body
	// required: true
	Body createUserRequest
}

// swagger:parameters getUser deleteUser changeUserPassword
type usernamePathParams struct {
	// Username.
	//
	// in: path
	// required: true
	Username string `json:"username"`
}

// swagger:parameters changeOwnPassword
type changeOwnPasswordParams struct {
	// Password change request.
	//
	// in: body
	// required: true
	Body changePasswordRequest
}

// swagger:parameters changeUserPassword
type changeUserPasswordParams struct {
	// Password reset request.
	//
	// in: body
	// required: true
	Body changeUserPasswordRequest
}

// swagger:parameters syncGroup
type syncGroupParams struct {
	// Group state artifact payload.
	//
	// in: body
	// required: true
	Body gcmodels.StateArtifact
}

// swagger:parameters getGroup deleteGroup listGroupSatellites
type groupPathParams struct {
	// Group name.
	//
	// in: path
	// required: true
	Group string `json:"group"`
}

// swagger:parameters addSatelliteToGroup removeSatelliteFromGroup
type satelliteGroupParams struct {
	// Satellite and group membership payload.
	//
	// in: body
	// required: true
	Body SatelliteGroupParams
}

// swagger:parameters createConfig
type createConfigParams struct {
	// Configuration creation payload.
	//
	// in: body
	// required: true
	Body APIConfigObject
}

// swagger:parameters getConfig updateConfig deleteConfig
type configPathParams struct {
	// Configuration name.
	//
	// in: path
	// required: true
	Config string `json:"config"`
}

// swagger:parameters updateConfig
type updateConfigParams struct {
	// Configuration merge patch payload.
	//
	// in: body
	// required: true
	Body APIConfigValue
}

// swagger:parameters setSatelliteConfig
type setSatelliteConfigParams struct {
	// Satellite configuration assignment payload.
	//
	// in: body
	// required: true
	Body SatelliteConfigParams
}

// swagger:parameters registerSatellite
type registerSatelliteParams struct {
	// Satellite registration payload.
	//
	// in: body
	// required: true
	Body RegisterSatelliteParams
}

// swagger:parameters getSatellite deleteSatellite getSatelliteStatus getCachedImages
type satellitePathParams struct {
	// Satellite name.
	//
	// in: path
	// required: true
	Satellite string `json:"satellite"`
}

// swagger:parameters listSpireAgents
type listSpireAgentsParams struct {
	// Filters agents by attestation type.
	//
	// in: query
	AttestationType string `json:"attestation_type"`
}

// swagger:parameters registerSatelliteWithSpiffe
type registerSatelliteWithSpiffeParams struct {
	// SPIFFE satellite registration payload.
	//
	// in: body
	// required: true
	Body RegisterSatelliteRequest
}

// swagger:parameters ztr
type ztrParams struct {
	// Single-use zero-touch registration token.
	//
	// in: path
	// required: true
	Token string `json:"token"`
}

// swagger:parameters syncSatellite
type syncSatelliteParams struct {
	// Satellite status report payload.
	//
	// in: body
	// required: true
	Body SatelliteStatusParams
}

// APIHealthResponse contains server health status.
//
// swagger:model APIHealthResponse
type APIHealthResponse struct {
	Status string `json:"status"`
}

// APIMessageResponse contains a human-readable command result.
//
// swagger:model APIMessageResponse
type APIMessageResponse struct {
	Message string `json:"message"`
}

// APIEmptyObject is an empty JSON object response.
//
// swagger:model APIEmptyObject
type APIEmptyObject struct{}

// APIRegistryCredentials contains registry credentials returned to a satellite.
//
// swagger:model APIRegistryCredentials
type APIRegistryCredentials struct {
	URL      string          `json:"url,omitempty"`
	Username string          `json:"username,omitempty"`
	Password swaggerPassword `json:"password,omitempty"`
}

// APIStateConfig contains state artifact and registry auth data.
//
// swagger:model APIStateConfig
type APIStateConfig struct {
	Auth  APIRegistryCredentials `json:"auth,omitempty"`
	State string                 `json:"state,omitempty"`
}

// APIConfigValue contains a satellite configuration document.
//
// swagger:model APIConfigValue
type APIConfigValue struct {
	StateConfig map[string]any `json:"state_config,omitempty"`
	AppConfig   map[string]any `json:"app_config,omitempty"`
	ZotConfig   map[string]any `json:"zot_config,omitempty"`
}

// APIConfigObject wraps a named satellite configuration request.
//
// swagger:model APIConfigObject
type APIConfigObject struct {
	ConfigName string         `json:"config_name,omitempty"`
	Registry   string         `json:"registry,omitempty"`
	Config     APIConfigValue `json:"config,omitempty"`
}

// APIDatabaseArtifact describes a cached image artifact row.
//
// swagger:model APIDatabaseArtifact
type APIDatabaseArtifact struct {
	// swagger:allOf
	dbmodels.Artifact
}

// APIDatabaseConfig describes a stored satellite configuration row.
//
// swagger:model APIDatabaseConfig
type APIDatabaseConfig struct {
	// swagger:allOf
	dbmodels.Config
}

// APIGroup describes a satellite group row.
//
// swagger:model APIGroup
type APIGroup struct {
	// swagger:allOf
	dbmodels.Group
}

// APIGroupSatellite describes a satellite attached to a group.
//
// swagger:model APIGroupSatellite
type APIGroupSatellite struct {
	// swagger:allOf
	dbmodels.GetSatellitesByGroupNameRow
}

// APISatellite describes a satellite row.
//
// swagger:model APISatellite
type APISatellite struct {
	// swagger:allOf
	dbmodels.Satellite
}

// APIActiveSatellite describes an active satellite row.
//
// swagger:model APIActiveSatellite
type APIActiveSatellite struct {
	// swagger:allOf
	dbmodels.GetActiveSatellitesRow
}

// APIStaleSatellite describes a stale satellite row.
//
// swagger:model APIStaleSatellite
type APIStaleSatellite struct {
	// swagger:allOf
	dbmodels.GetStaleSatellitesRow
}

// APISatelliteStatus describes a stored satellite status report.
//
// swagger:model APISatelliteStatus
type APISatelliteStatus struct {
	// swagger:allOf
	dbmodels.SatelliteStatus
}

// Reusable response schema.
//
// swagger:response plainTextResponse
type plainTextResponse struct {
	// in: body
	Body string
}

// Reusable response schema.
//
// swagger:response noContentResponse
type noContentResponse struct{}

// Reusable response schema.
//
// swagger:response errorResponse
type errorResponse struct {
	// in: body
	Body AppError
}

// Reusable response schema.
//
// swagger:response healthResponse
type healthResponse struct {
	// in: body
	Body APIHealthResponse
}

// Reusable response schema.
//
// swagger:response emptyObjectResponse
type emptyObjectResponse struct {
	// in: body
	Body APIEmptyObject
}

// Reusable response schema.
//
// swagger:response messageResponse
type messageResponse struct {
	// in: body
	Body APIMessageResponse
}

// Reusable response schema.
//
// swagger:response loginResponse
type loginResponseDoc struct {
	// in: body
	Body loginResponse
}

// Reusable response schema.
//
// swagger:response userResponse
type userResponseDoc struct {
	// in: body
	Body userResponse
}

// Reusable response schema.
//
// swagger:response usersResponse
type usersResponse struct {
	// in: body
	Body []userResponse
}

// Reusable response schema.
//
// swagger:response groupResponse
type groupResponse struct {
	// in: body
	Body APIGroup
}

// Reusable response schema.
//
// swagger:response groupsResponse
type groupsResponse struct {
	// in: body
	Body []APIGroup
}

// Reusable response schema.
//
// swagger:response groupSatellitesResponse
type groupSatellitesResponse struct {
	// in: body
	Body []APIGroupSatellite
}

// Reusable response schema.
//
// swagger:response configResponse
type configResponse struct {
	// in: body
	Body APIDatabaseConfig
}

// Reusable response schema.
//
// swagger:response configsResponse
type configsResponse struct {
	// in: body
	Body []APIDatabaseConfig
}

// Reusable response schema.
//
// swagger:response registerSatelliteResponse
type registerSatelliteResponseDoc struct {
	// in: body
	Body RegisterSatelliteResponse
}

// Reusable response schema.
//
// swagger:response satelliteResponse
type satelliteResponse struct {
	// in: body
	Body APISatellite
}

// Reusable response schema.
//
// swagger:response satellitesResponse
type satellitesResponse struct {
	// in: body
	Body []APISatellite
}

// Reusable response schema.
//
// swagger:response activeSatellitesResponse
type activeSatellitesResponse struct {
	// in: body
	Body []APIActiveSatellite
}

// Reusable response schema.
//
// swagger:response staleSatellitesResponse
type staleSatellitesResponse struct {
	// in: body
	Body []APIStaleSatellite
}

// Reusable response schema.
//
// swagger:response satelliteStatusResponse
type satelliteStatusResponse struct {
	// in: body
	Body APISatelliteStatus
}

// Reusable response schema.
//
// swagger:response cachedImagesResponse
type cachedImagesResponse struct {
	// in: body
	Body []APIDatabaseArtifact
}

// Reusable response schema.
//
// swagger:response spireStatusResponse
type spireStatusResponse struct {
	// in: body
	Body SPIREStatusResponse
}

// Reusable response schema.
//
// swagger:response spireAgentsResponse
type spireAgentsResponse struct {
	// in: body
	Body AgentListResponse
}

// Reusable response schema.
//
// swagger:response registerSatelliteWithSpiffeResponse
type registerSatelliteWithSpiffeResponse struct {
	// in: body
	Body RegisterSatelliteWithSPIFFEResponse
}

// Reusable response schema.
//
// swagger:response stateConfigResponse
type stateConfigResponse struct {
	// in: body
	Body APIStateConfig
}
