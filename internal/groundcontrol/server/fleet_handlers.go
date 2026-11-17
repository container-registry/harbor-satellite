package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	auditlog "github.com/container-registry/harbor-satellite/internal/shared/logger"
)

// The handlers below decode, call one fleet service and encode the API
// type (plan item 1.4). The response bodies are the sqlc rows today's
// clients see, so these row types keep their field names.

type configRow struct {
	ID          int32
	ConfigName  string
	RegistryUrl string //nolint:revive // the JSON name clients see
	Config      json.RawMessage
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// Warnings are the validation warnings of a create or update.
	Warnings []string `json:"warnings,omitempty"`
}

func toConfigRow(c fleet.Config) configRow {
	return configRow{ID: int32(c.ID), ConfigName: c.Name, RegistryUrl: c.RegistryURL, Config: c.Document, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

type groupRow struct {
	ID          int32
	GroupName   string
	RegistryUrl string //nolint:revive // the JSON name clients see
	Projects    []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func toGroupRow(g fleet.Group) groupRow {
	projects := g.Projects
	if projects == nil {
		projects = []string{}
	}
	return groupRow{ID: int32(g.ID), GroupName: g.Name, RegistryUrl: g.RegistryURL, Projects: projects, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt}
}

type satelliteRow struct {
	ID                int32
	Name              string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	LastSeen          sql.NullTime
	HeartbeatInterval sql.NullString
}

func toSatelliteRow(s fleet.Satellite) satelliteRow {
	return satelliteRow{
		ID: int32(s.ID), Name: s.Name, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
		LastSeen:          sql.NullTime{Time: s.LastSeen, Valid: !s.LastSeen.IsZero()},
		HeartbeatInterval: sql.NullString{String: s.HeartbeatInterval, Valid: s.HeartbeatInterval != ""},
	}
}

type groupMemberRow struct {
	ID        int32
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// services returns the fleet services; NewServer refuses to start
// without them.
func (s *Server) svc() *fleet.Services { return s.services }

func (s *Server) CreateConfig(w http.ResponseWriter, r *http.Request) {
	var req ConfigCreateRequest
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}
	if !fleet.ValidName(req.ConfigName) {
		HandleAppError(w, &AppError{Message: "invalid or empty config_name", Code: http.StatusBadRequest})
		return
	}
	doc, err := json.Marshal(req.Config)
	if err != nil {
		HandleAppError(w, err)
		return
	}
	res, err := s.svc().Configs.Create(r.Context(), fleet.CreateConfigCmd{Name: req.ConfigName, Document: doc})
	if err != nil {
		log.Printf("Error creating config %s: %v", req.ConfigName, err)
		if writeConfigValidationError(w, err) {
			return
		}
		if errors.Is(err, fleet.ErrAlreadyExists) {
			HandleAppError(w, &AppError{Message: "error: config already exists", Code: http.StatusConflict})
			return
		}
		handleFleetError(w, err, nil)
		return
	}
	s.auditEvent(r, auditlog.AuditEvent{
		Operation: auditlog.OpCreate, ResourceType: auditlog.ResConfig, Outcome: auditlog.OutcomeSuccess,
		Actor: actorFromContext(r.Context()), ActorType: auditlog.ActorUser, Resource: req.ConfigName,
		Details: map[string]any{"to": redactConfigForAudit(doc)},
	})
	row := toConfigRow(res.Config)
	row.Warnings = res.Warnings
	WriteJSONResponse(w, http.StatusCreated, row)
}

// writeConfigValidationError answers a config that failed validation
// with 422 and its warnings (plan item 7.4).
func writeConfigValidationError(w http.ResponseWriter, err error) bool {
	var verr *fleet.ConfigValidationError
	if !errors.As(err, &verr) {
		return false
	}
	WriteJSONResponse(w, http.StatusUnprocessableEntity, ConfigValidationError{
		Code: http.StatusUnprocessableEntity, Message: "invalid config: " + verr.Err.Error(), Warnings: verr.Warnings,
	})
	return true
}

func (s *Server) UpdateConfig(w http.ResponseWriter, r *http.Request, configName string) {
	var req ConfigMergePatch
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}
	if !fleet.ValidName(configName) {
		HandleAppError(w, &AppError{Message: "invalid or empty config_name", Code: http.StatusBadRequest})
		return
	}
	patch, err := json.Marshal(req)
	if err != nil {
		HandleAppError(w, err)
		return
	}
	res, err := s.svc().Configs.Update(r.Context(), fleet.UpdateConfigCmd{Name: configName, Patch: patch})
	if err != nil {
		log.Printf("Error updating config %s: %v", configName, err)
		if writeConfigValidationError(w, err) {
			return
		}
		switch {
		case errors.Is(err, fleet.ErrNotFound):
			HandleAppError(w, &AppError{Message: "error: config not found", Code: http.StatusNotFound})
		case errors.Is(err, fleet.ErrConflict):
			HandleAppError(w, &AppError{Message: "error: the config was changed by another request; fetch it and retry", Code: http.StatusConflict})
		case errors.Is(err, fleet.ErrInvalid):
			HandleAppError(w, &AppError{Message: "error: unable to apply patch", Code: http.StatusBadRequest})
		default:
			handleFleetError(w, err, nil)
		}
		return
	}
	event := auditlog.AuditEvent{
		Operation: auditlog.OpUpdate, ResourceType: auditlog.ResConfig, Outcome: auditlog.OutcomeSuccess,
		Actor: actorFromContext(r.Context()), ActorType: auditlog.ActorUser, Resource: configName,
	}
	if changed := diffConfigForAudit(res.Previous.Document, res.Config.Document); len(changed) > 0 {
		event.Details = map[string]any{"changed": changed}
	}
	s.auditEvent(r, event)
	row := toConfigRow(res.Config)
	row.Warnings = res.Warnings
	WriteJSONResponse(w, http.StatusOK, row)
}

func (s *Server) ListConfigs(w http.ResponseWriter, r *http.Request) {
	configs, err := s.svc().Configs.List(r.Context())
	if err != nil {
		log.Printf("Could not list configs: %v", err)
		HandleAppError(w, err)
		return
	}
	out := make([]configRow, 0, len(configs))
	for _, c := range configs {
		out = append(out, toConfigRow(c))
	}
	WriteJSONResponse(w, http.StatusOK, out)
}

func (s *Server) GetConfig(w http.ResponseWriter, r *http.Request, configName string) {
	cfg, err := s.svc().Configs.Get(r.Context(), configName)
	if err != nil {
		if errors.Is(err, fleet.ErrNotFound) {
			HandleAppError(w, &AppError{Message: "Config not found", Code: http.StatusNotFound})
			return
		}
		HandleAppError(w, err)
		return
	}
	WriteJSONResponse(w, http.StatusOK, toConfigRow(cfg))
}

func (s *Server) SetSatelliteConfig(w http.ResponseWriter, r *http.Request) {
	var req SatelliteConfigRequest
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}
	if err := s.svc().Configs.Assign(r.Context(), fleet.AssignConfigCmd{Satellite: req.Satellite, Config: req.ConfigName}); err != nil {
		log.Printf("Error assigning config %s to %s: %v", req.ConfigName, req.Satellite, err)
		if errors.Is(err, fleet.ErrNotFound) {
			handleFleetError(w, err, nil)
			return
		}
		HandleAppError(w, &AppError{Message: "Error: Failed to Add satellite to config", Code: http.StatusInternalServerError})
		return
	}
	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}

func (s *Server) DeleteConfig(w http.ResponseWriter, r *http.Request, configName string) {
	cfg, err := s.svc().Configs.Get(r.Context(), configName)
	if err != nil {
		HandleAppError(w, &AppError{Message: "Error: Failed to get Config", Code: http.StatusInternalServerError})
		return
	}
	if err := s.svc().Configs.Delete(r.Context(), configName); err != nil {
		if errors.Is(err, fleet.ErrInUse) {
			HandleAppError(w, &AppError{Message: "Cannot delete config that is in use", Code: http.StatusBadRequest})
			return
		}
		log.Printf("Error deleting config %s: %v", configName, err)
		handleFleetError(w, err, nil)
		return
	}
	s.auditEvent(r, auditlog.AuditEvent{
		Operation: auditlog.OpDelete, ResourceType: auditlog.ResConfig, Outcome: auditlog.OutcomeSuccess,
		Actor: actorFromContext(r.Context()), ActorType: auditlog.ActorUser, Resource: configName,
		Details: map[string]any{"from": redactConfigForAudit(cfg.Document)},
	})
	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}

func toFleetArtifacts(in []GroupArtifact) []fleet.Artifact {
	out := make([]fleet.Artifact, 0, len(in))
	for _, a := range in {
		out = append(out, fleet.Artifact{Deleted: a.Deleted, Digest: a.Digest, Labels: a.Labels, Repository: a.Repository, Tags: a.Tag, Type: a.Type})
	}
	return out
}

func (s *Server) SyncGroup(w http.ResponseWriter, r *http.Request) {
	var req GroupSyncRequest
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}
	group, err := s.svc().Groups.Sync(r.Context(), fleet.SyncGroupCmd{Name: req.Group, Artifacts: toFleetArtifacts(req.Artifacts)})
	if err != nil {
		log.Printf("Error syncing group %s: %v", req.Group, err)
		handleFleetError(w, err, nil)
		return
	}
	WriteJSONResponse(w, http.StatusOK, toGroupRow(group))
}

func (s *Server) GetGroup(w http.ResponseWriter, r *http.Request, group string) {
	g, err := s.svc().Groups.Get(r.Context(), group)
	if err != nil {
		if errors.Is(err, fleet.ErrNotFound) {
			HandleAppError(w, &AppError{Message: "Group not found", Code: http.StatusNotFound})
			return
		}
		log.Printf("Could not get group: %v", err)
		HandleAppError(w, err)
		return
	}
	WriteJSONResponse(w, http.StatusOK, toGroupRow(g))
}

func (s *Server) DeleteGroup(w http.ResponseWriter, r *http.Request, groupName string) {
	if err := s.svc().Groups.Delete(r.Context(), groupName); err != nil {
		log.Printf("Error deleting group %s: %v", groupName, err)
		if errors.Is(err, fleet.ErrNotFound) {
			HandleAppError(w, &AppError{Message: "Error: Group Not Found", Code: http.StatusNotFound})
			return
		}
		HandleAppError(w, &AppError{Message: "Error: Failed to delete group", Code: http.StatusInternalServerError})
		return
	}
	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}

func (s *Server) ListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.svc().Groups.List(r.Context())
	if err != nil {
		log.Printf("Could not list groups: %v", err)
		HandleAppError(w, err)
		return
	}
	out := make([]groupRow, 0, len(groups))
	for _, g := range groups {
		out = append(out, toGroupRow(g))
	}
	WriteJSONResponse(w, http.StatusOK, out)
}

func (s *Server) ListGroupSatellites(w http.ResponseWriter, r *http.Request, groupName string) {
	members, err := s.svc().Groups.Members(r.Context(), groupName)
	if err != nil {
		if errors.Is(err, fleet.ErrNotFound) {
			HandleAppError(w, &AppError{Message: "error: group not found", Code: http.StatusNotFound})
			return
		}
		HandleAppError(w, &AppError{Message: "error: failed to get satellites for group", Code: http.StatusInternalServerError})
		return
	}
	out := make([]groupMemberRow, 0, len(members))
	for _, m := range members {
		out = append(out, groupMemberRow{ID: int32(m.ID), Name: m.Name, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt})
	}
	WriteJSONResponse(w, http.StatusOK, out)
}

func (s *Server) RegisterSatellite(w http.ResponseWriter, r *http.Request) {
	if s.spiffeProvider != nil || s.spireClient != nil {
		HandleAppError(w, &AppError{
			Message: "satellite registration via this endpoint is disabled when SPIFFE is enabled. Use POST /api/satellites/register instead",
			Code:    http.StatusBadRequest,
		})
		return
	}
	var req TokenSatelliteRegistrationRequest
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}
	if !fleet.ValidName(req.Name) {
		HandleAppError(w, &AppError{Message: fmt.Sprintf(invalidNameMessage, "satellite"), Code: http.StatusBadRequest})
		return
	}
	if !fleet.ValidName(req.ConfigName) {
		HandleAppError(w, &AppError{Message: "invalid or empty config_name", Code: http.StatusBadRequest})
		return
	}
	tok, err := s.svc().Satellites.Register(r.Context(), fleet.RegisterSatelliteCmd{Name: req.Name, ConfigName: req.ConfigName, Groups: req.Groups})
	if err != nil {
		log.Printf("Error registering satellite %s: %v", req.Name, err)
		if errors.Is(err, fleet.ErrAlreadyExists) {
			HandleAppError(w, &AppError{Message: "Error: Robot Account name already present. Try with different name", Code: http.StatusBadRequest})
			return
		}
		handleFleetError(w, err, nil)
		return
	}
	s.auditEvent(r, auditlog.AuditEvent{
		Operation: auditlog.OpRegister, ResourceType: auditlog.ResSatellite, Outcome: auditlog.OutcomeSuccess,
		Actor: actorFromContext(r.Context()), ActorType: auditlog.ActorUser, SatelliteID: req.Name,
		Details: map[string]any{"config_name": req.ConfigName, "groups": req.Groups, "flow": "token"},
	})
	WriteJSONResponse(w, http.StatusOK, TokenSatelliteRegistrationResponse{Token: tok.Token})
}

func stateConfigResponse(res fleet.ZTRResult) StateConfigResponse {
	out := StateConfigResponse{
		State: res.StateURL,
		Auth:  RegistryCredentials{Username: res.Credentials.Name, Password: res.Credentials.Secret, URL: res.RegistryURL},
	}
	if res.Session != nil {
		out.Session = &SatelliteSession{Token: res.Session.Token, ExpiresAt: res.Session.ExpiresAt}
	}
	return out
}

// Ztr answers 503 with Retry-After until the async plane created the
// satellite's robot; the token stays unclaimed for the retry.
func (s *Server) Ztr(w http.ResponseWriter, r *http.Request) {
	var request ZTRRequest
	if err := DecodeRequestBody(r, &request); err != nil {
		HandleAppError(w, err)
		return
	}
	res, err := s.svc().Registration.ZeroTouch(r.Context(), request.Token)
	if err != nil {
		if errors.Is(err, fleet.ErrTokenInvalid) || errors.Is(err, fleet.ErrTokenExpired) {
			s.rejectZTRToken(w, r, request.Token, err)
			return
		}
		log.Printf("ZTR: token %s: %v", maskToken(request.Token), err)
		handleFleetError(w, err, ztrFailure(err))
		return
	}
	s.auditEvent(r, auditlog.AuditEvent{
		Operation: auditlog.OpRegister, ResourceType: auditlog.ResSatellite, Outcome: auditlog.OutcomeSuccess,
		Actor: res.SatelliteName, ActorType: auditlog.ActorSatellite, SatelliteID: res.SatelliteName,
		Details: map[string]any{"flow": "ztr"},
	})
	WriteJSONResponse(w, http.StatusOK, stateConfigResponse(res))
}

// ztrFailure is the answer to a registration that failed after the
// identity was accepted: 503 while the robot is not provisioned or the
// registry is unreachable, so the satellite retries.
func ztrFailure(err error) *AppError {
	switch {
	case errors.Is(err, fleet.ErrNotProvisioned):
		return &AppError{Message: "Error: satellite is not provisioned yet; retry later", Code: http.StatusServiceUnavailable}
	case errors.Is(err, fleet.ErrRegistryUnavailable):
		return &AppError{Message: "Error: registry unavailable; retry later", Code: http.StatusServiceUnavailable}
	case errors.Is(err, fleet.ErrRevoked):
		return &AppError{Message: "Error: satellite revoked", Code: http.StatusForbidden}
	default:
		return &AppError{Message: "Error: failed to refresh robot secret", Code: http.StatusInternalServerError}
	}
}

// SpiffeZtr handles Zero-Touch Registration using SPIFFE mTLS
// authentication: the satellite's identity is its SVID.
func (s *Server) SpiffeZtr(w http.ResponseWriter, r *http.Request) {
	satelliteName, ok := spiffe.GetSatelliteName(r.Context())
	if !ok {
		spiffeID, ok := spiffe.GetSPIFFEID(r.Context())
		if !ok {
			s.auditEvent(r, auditlog.AuditEvent{
				Operation: auditlog.OpAuth, ResourceType: auditlog.ResSatellite, Outcome: auditlog.OutcomeFailure,
				ActorType: auditlog.ActorAnonymous, Reason: auditlog.ReasonMissingSpiffeIdentity,
				Details: map[string]any{"flow": "spiffe_ztr"},
			})
			HandleAppError(w, &AppError{Message: "Error: SPIFFE authentication required", Code: http.StatusUnauthorized})
			return
		}
		var err error
		satelliteName, err = spiffe.ExtractSatelliteNameFromSPIFFEID(spiffeID)
		if err != nil {
			s.auditEvent(r, auditlog.AuditEvent{
				Operation: auditlog.OpAuth, ResourceType: auditlog.ResSatellite, Outcome: auditlog.OutcomeFailure,
				Actor: spiffeID.String(), ActorType: auditlog.ActorAnonymous, Reason: auditlog.ReasonInvalidSpiffeID,
				Details: map[string]any{"flow": "spiffe_ztr"},
			})
			HandleAppError(w, &AppError{Message: "Error: Invalid SPIFFE ID format for satellite", Code: http.StatusBadRequest})
			return
		}
	}
	res, err := s.svc().Registration.SPIFFEZeroTouch(r.Context(), satelliteName)
	if err != nil {
		log.Printf("SPIFFE ZTR: satellite %s: %v", satelliteName, err)
		handleFleetError(w, err, ztrFailure(err))
		return
	}
	s.auditEvent(r, auditlog.AuditEvent{
		Operation: auditlog.OpRegister, ResourceType: auditlog.ResSatellite, Outcome: auditlog.OutcomeSuccess,
		Actor: res.SatelliteName, ActorType: auditlog.ActorSatellite, SatelliteID: res.SatelliteName,
		Details: map[string]any{"flow": "spiffe_ztr"},
	})
	WriteJSONResponse(w, http.StatusOK, stateConfigResponse(res))
}

func (s *Server) ListSatellites(w http.ResponseWriter, r *http.Request) {
	sats, err := s.svc().Satellites.List(r.Context())
	if err != nil {
		log.Printf("Error: Failed to List Satellites: %v", err)
		HandleAppError(w, &AppError{Message: "Error: Failed to List Satellites", Code: http.StatusInternalServerError})
		return
	}
	out := make([]satelliteRow, 0, len(sats))
	for _, sat := range sats {
		out = append(out, toSatelliteRow(sat))
	}
	WriteJSONResponse(w, http.StatusOK, out)
}

func (s *Server) GetSatellite(w http.ResponseWriter, r *http.Request, satellite string) {
	sat, err := s.svc().Satellites.Get(r.Context(), satellite)
	if err != nil {
		if errors.Is(err, fleet.ErrNotFound) {
			HandleAppError(w, &AppError{Message: "Error: Satellite Not Found", Code: http.StatusNotFound})
			return
		}
		log.Printf("Error getting satellite %s: %v", satellite, err)
		HandleAppError(w, &AppError{Message: "Error: Failed to Get Satellite", Code: http.StatusInternalServerError})
		return
	}
	WriteJSONResponse(w, http.StatusOK, toSatelliteRow(sat))
}

func (s *Server) DeleteSatellite(w http.ResponseWriter, r *http.Request, satellite string) {
	if err := s.svc().Satellites.Delete(r.Context(), satellite); err != nil {
		log.Printf("Error deleting satellite %s: %v", satellite, err)
		if errors.Is(err, fleet.ErrNotFound) {
			HandleAppError(w, &AppError{Message: "Error: Satellite Not Found", Code: http.StatusNotFound})
			return
		}
		HandleAppError(w, &AppError{Message: "Error: Failed to Delete Satellite", Code: http.StatusInternalServerError})
		return
	}
	actor := "unknown"
	if u, ok := GetUserFromContext(r.Context()); ok {
		actor = u.Username
	}
	s.auditEvent(r, auditlog.AuditEvent{
		Operation: auditlog.OpDeregister, ResourceType: auditlog.ResSatellite, Outcome: auditlog.OutcomeSuccess,
		Actor: actor, ActorType: auditlog.ActorUser, SatelliteID: satellite,
	})
	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}

// membershipError keeps today's answers for an unknown satellite or group.
func (s *Server) membershipError(w http.ResponseWriter, r *http.Request, req SatelliteGroupRequest) bool {
	if _, err := s.svc().Satellites.Get(r.Context(), req.Satellite); err != nil {
		HandleAppError(w, &AppError{Message: "Error: Satellite Not Found", Code: http.StatusBadRequest})
		return true
	}
	if _, err := s.svc().Groups.Get(r.Context(), req.Group); err != nil {
		HandleAppError(w, &AppError{Message: "Error: Group Not Found", Code: http.StatusBadRequest})
		return true
	}
	return false
}

func (s *Server) AddSatelliteToGroup(w http.ResponseWriter, r *http.Request) {
	var req SatelliteGroupRequest
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}
	if !fleet.ValidName(req.Satellite) {
		HandleAppError(w, &AppError{Message: fmt.Sprintf(invalidNameMessage, "satellite"), Code: http.StatusBadRequest})
		return
	}
	if s.membershipError(w, r, req) {
		return
	}
	cmd := fleet.MembershipCmd{Satellite: req.Satellite, Group: req.Group}
	member, err := s.svc().Groups.HasMember(r.Context(), cmd)
	if err != nil {
		HandleAppError(w, &AppError{Message: "Error: Failed to check satellite in group", Code: http.StatusInternalServerError})
		return
	}
	if member {
		WriteJSONResponse(w, http.StatusOK, map[string]string{"message": "Satellite is already in the group"})
		return
	}
	if err := s.svc().Groups.AddSatellite(r.Context(), cmd); err != nil {
		log.Printf("Error adding %s to group %s: %v", req.Satellite, req.Group, err)
		HandleAppError(w, &AppError{Message: "Error: Failed to add satellite to group", Code: http.StatusInternalServerError})
		return
	}
	WriteJSONResponse(w, http.StatusOK, map[string]string{"message": "Satellite successfully added to group"})
}

func (s *Server) RemoveSatelliteFromGroup(w http.ResponseWriter, r *http.Request) {
	var req SatelliteGroupRequest
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}
	if s.membershipError(w, r, req) {
		return
	}
	if err := s.svc().Groups.RemoveSatellite(r.Context(), fleet.MembershipCmd{Satellite: req.Satellite, Group: req.Group}); err != nil {
		log.Printf("Error removing %s from group %s: %v", req.Satellite, req.Group, err)
		HandleAppError(w, &AppError{Message: "Error: Failed to Remove Satellite from Group", Code: http.StatusInternalServerError})
		return
	}
	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}

// RefreshSatelliteCredentials serves POST /satellites/refresh. The
// satellite auth chain has already authenticated the caller by SVID or
// robot Basic auth and put its name in the context.
func (s *Server) RefreshSatelliteCredentials(w http.ResponseWriter, r *http.Request) {
	satelliteName, ok := spiffe.GetSatelliteName(r.Context())
	if !ok {
		HandleAppError(w, &AppError{Message: "unknown satellite entity", Code: http.StatusForbidden})
		return
	}
	creds, err := s.svc().Registration.RefreshCredentials(r.Context(), satelliteName)
	if err != nil {
		log.Printf("Failed to refresh robot secret for satellite %s: %v", satelliteName, err)
		switch {
		case errors.Is(err, fleet.ErrNotFound):
			HandleAppError(w, &AppError{Message: "unknown satellite entity", Code: http.StatusForbidden})
		case errors.Is(err, fleet.ErrNotProvisioned):
			HandleAppError(w, &AppError{Message: "Failed to find robot account", Code: http.StatusForbidden})
		default:
			HandleAppError(w, &AppError{Message: "Error: failed to refresh robot secret", Code: http.StatusInternalServerError})
		}
		return
	}
	WriteJSONResponse(w, http.StatusOK, refreshCredentialResponse{Secret: creds.Secret})
}

// RegisterSatelliteWithSpiffe creates a satellite's SPIRE registration.
func (s *Server) RegisterSatelliteWithSpiffe(w http.ResponseWriter, r *http.Request) {
	var req SPIFFESatelliteRegistrationRequest
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, &AppError{Message: "Invalid request body", Code: http.StatusBadRequest})
		return
	}
	if appErr := validateSPIFFERegistration(&req); appErr != nil {
		HandleAppError(w, appErr)
		return
	}
	ttl := 600 * time.Second
	if req.TTLSeconds != nil {
		ttl = time.Duration(*req.TTLSeconds) * time.Second
	}
	if s.spireClient == nil {
		HandleAppError(w, &AppError{Message: "SPIRE server not configured", Code: http.StatusServiceUnavailable})
		return
	}
	reg, err := s.svc().Registration.RegisterSPIFFE(r.Context(), fleet.RegisterSPIFFECmd{
		Name: req.SatelliteName, Region: req.Region, AttestationMethod: string(req.AttestationMethod),
		ParentAgentID: req.ParentAgentID, Selectors: req.Selectors, TTL: ttl,
	})
	if err != nil {
		log.Printf("Register: satellite %s: %v", req.SatelliteName, err)
		switch {
		case errors.Is(err, fleet.ErrNotFound):
			HandleAppError(w, &AppError{Message: fmt.Sprintf("no x509pop agent found with selector %q", "x509pop:subject:cn:"+req.SatelliteName), Code: http.StatusNotFound})
		case errors.Is(err, fleet.ErrNotProvisioned):
			HandleAppError(w, &AppError{Message: "SPIRE server not configured", Code: http.StatusServiceUnavailable})
		default:
			handleFleetError(w, err, nil)
		}
		return
	}
	resp := SPIFFESatelliteRegistrationResponse{
		Satellite: req.SatelliteName, Region: req.Region, SpiffeID: reg.SPIFFEID, ParentAgentID: reg.AgentID,
		SpireServerAddress: s.spireServerAddress, SpireServerPort: int64(s.spireServerPort), TrustDomain: s.spireTrustDomain,
	}
	if reg.JoinToken != "" {
		resp.JoinToken = optionalString(reg.JoinToken)
		expires := reg.ExpiresAt
		resp.ExpiresAt = &expires
	}
	WriteJSONResponse(w, http.StatusOK, resp)
}

// validateSPIFFERegistration checks the request as today's handler did and
// defaults the region.
func validateSPIFFERegistration(req *SPIFFESatelliteRegistrationRequest) *AppError {
	switch {
	case req.SatelliteName == "":
		return &AppError{Message: "satellite_name is required", Code: http.StatusBadRequest}
	case len(req.Selectors) == 0:
		return &AppError{Message: "selectors is required", Code: http.StatusBadRequest}
	}
	for _, sel := range req.Selectors {
		if !strings.Contains(sel, ":") {
			return &AppError{Message: fmt.Sprintf("invalid selector format %q: must contain ':'", sel), Code: http.StatusBadRequest}
		}
	}
	if !req.AttestationMethod.Valid() {
		return &AppError{Message: "attestation_method must be one of: join_token, x509pop, sshpop", Code: http.StatusBadRequest}
	}
	if req.AttestationMethod == Sshpop && req.ParentAgentID == "" {
		return &AppError{Message: "parent_agent_id is required for sshpop attestation", Code: http.StatusBadRequest}
	}
	if req.Region == "" {
		req.Region = "default"
	}
	if req.TTLSeconds != nil && (*req.TTLSeconds < 1 || *req.TTLSeconds > 86400) {
		return &AppError{Message: "ttl_seconds must be between 1 and 86400", Code: http.StatusBadRequest}
	}
	return nil
}
