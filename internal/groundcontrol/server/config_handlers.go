package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"strings"

	"github.com/container-registry/harbor-satellite/internal/env"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	auditlog "github.com/container-registry/harbor-satellite/internal/groundcontrol/logger"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/models"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/utils"
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/lib/pq"
)

// SatelliteConfigParams links a satellite to a named configuration.
//
// swagger:model SatelliteConfigParams
type SatelliteConfigParams struct {
	Satellite  string `json:"satellite,omitempty"`
	ConfigName string `json:"config_name"`
}

// auditRedacted is the placeholder substituted for sensitive config values
// before a config is recorded in the audit log.
const auditRedacted = "[REDACTED]"

// isSensitiveConfigKey reports whether a config key names a secret that must not
// be written to the audit log. Matched case-insensitively as a substring so
// variants (e.g. "password", "secret_key") are all caught.
func isSensitiveConfigKey(key string) bool {
	k := strings.ToLower(key)
	for _, frag := range []string{"password", "passwd", "secret", "token", "credential", "apikey", "api_key", "access_key", "private_key"} {
		if strings.Contains(k, frag) {
			return true
		}
	}
	return false
}

// redactConfigValue returns a deep copy of a decoded JSON value with the values
// of sensitive keys replaced. It recurses through objects and arrays.
func redactConfigValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if isSensitiveConfigKey(k) {
				out[k] = auditRedacted
			} else {
				out[k] = redactConfigValue(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redactConfigValue(val)
		}
		return out
	default:
		return v
	}
}

// redactConfigForAudit decodes raw config JSON and returns a secret-free copy
// safe to embed in an audit event's details.from / details.to. On empty input
// it returns nil; if the JSON cannot be decoded it returns a placeholder rather
// than the raw bytes, so secrets can never leak through a parse failure.
func redactConfigForAudit(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "[unparseable config]"
	}
	return redactConfigValue(decoded)
}

// childPath joins a dotted config path. The root prefix is empty.
func childPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// lastKey returns the final segment of a dotted path, used to decide whether a
// changed value is sensitive.
func lastKey(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i+1:]
	}
	return path
}

// redactedLeaf returns a changed value safe for the audit log: sensitive keys
// collapse to the placeholder, other values keep their (recursively redacted)
// content so the change is visible.
func redactedLeaf(key string, v any) any {
	if isSensitiveConfigKey(key) {
		return auditRedacted
	}
	return redactConfigValue(v)
}

// diffConfigValues records, into out, every leaf path under prefix whose value
// differs between old and new. Nested objects are compared key-by-key; other
// values are compared whole. A changed leaf is recorded as {from, to} (either
// side omitted when the key was added or removed), with values redacted.
func diffConfigValues(out map[string]any, prefix string, oldV, newV any) {
	oldMap, oldIsMap := oldV.(map[string]any)
	newMap, newIsMap := newV.(map[string]any)
	if oldIsMap && newIsMap {
		diffConfigMaps(out, prefix, oldMap, newMap)
		return
	}
	if !reflect.DeepEqual(oldV, newV) {
		k := lastKey(prefix)
		out[prefix] = map[string]any{"from": redactedLeaf(k, oldV), "to": redactedLeaf(k, newV)}
	}
}

// diffConfigMaps recurses over the union of keys in two objects.
func diffConfigMaps(out map[string]any, prefix string, oldMap, newMap map[string]any) {
	for k, ov := range oldMap {
		path := childPath(prefix, k)
		if nv, ok := newMap[k]; ok {
			diffConfigValues(out, path, ov, nv)
		} else if ov != nil {
			// A key dropping from null to absent is not a real change.
			out[path] = map[string]any{"from": redactedLeaf(k, ov)}
		}
	}
	for k, nv := range newMap {
		if _, ok := oldMap[k]; ok {
			continue
		}
		if nv != nil {
			out[childPath(prefix, k)] = map[string]any{"to": redactedLeaf(k, nv)}
		}
	}
}

// diffConfigForAudit returns a flat map of changed config paths -> {from, to}
// (redacted), computed from the raw old and new config JSON. It returns nil when
// nothing changed. Because the comparison runs on the raw values, a rotated
// secret is reported as a changed path even though its value stays redacted.
func diffConfigForAudit(oldRaw, newRaw []byte) map[string]any {
	var oldV, newV any
	if err := json.Unmarshal(oldRaw, &oldV); err != nil {
		oldV = nil
	}
	if err := json.Unmarshal(newRaw, &newV); err != nil {
		newV = nil
	}
	out := map[string]any{}
	diffConfigValues(out, "", oldV, newV)
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *Server) CreateConfig(w http.ResponseWriter, r *http.Request) {
	var req models.ConfigObject

	if err := DecodeRequestBody(r, &req); err != nil {
		log.Println("Error decoding request body: ", err)
		HandleAppError(w, err)
		return
	}

	if !utils.IsValidName(req.ConfigName) {
		HandleAppError(w, &AppError{
			Message: "invalid or empty config_name",
			Code:    http.StatusBadRequest,
		})
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Printf("error starting transaction: %v", err)
		err := &AppError{
			Message: "error: failed to start database transaction",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}
	q := s.dbQueries.WithTx(tx)

	committed := false
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil {
				log.Printf("Error: Failed to rollback transaction for failed process: %v", err)
				err := &AppError{
					Message: "Error: Failed to rollback transaction",
					Code:    http.StatusInternalServerError,
				}
				HandleAppError(w, err)
				return
			}
		}
	}()

	configJson, err := json.Marshal(req.Config)
	if err != nil {
		log.Println("Could not marshal JSON: ", err)
		HandleAppError(w, err)
		return
	}

	if err := ensureSatelliteProjectExists(r.Context()); err != nil {
		log.Println("Error while ensuring project satellite: ", err)
		HandleAppError(w, err)
		return
	}

	params := database.CreateConfigParams{
		ConfigName:  req.ConfigName,
		RegistryUrl: env.GC.Harbor.URL,
		Config:      configJson,
	}

	_, err = q.CreateConfig(r.Context(), params)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			log.Printf("error: config with name '%s' already exists", req.ConfigName)
			HandleAppError(w, &AppError{
				Message: "error: config already exists",
				Code:    http.StatusConflict,
			})
			return
		}
		log.Println("Error persisting config object to database: ", err)
		HandleAppError(w, err)
		return
	}

	// Push config as OCI artifact
	err = utils.CreateAndPushConfigStateArtifact(r.Context(), configJson, req.ConfigName)
	if err != nil {
		log.Println("Error while creating config state artifact: ", err)
		HandleAppError(w, err)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Failed to commit transaction: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Failed to commit transaction",
			Code:    http.StatusInternalServerError,
		})
		return
	}
	committed = true

	s.auditEvent(r, auditlog.AuditEvent{
		Operation:    auditlog.OpCreate,
		ResourceType: auditlog.ResConfig,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        actorFromContext(r.Context()),
		ActorType:    auditlog.ActorUser,
		Resource:     req.ConfigName,
		Details:      map[string]any{"to": redactConfigForAudit(configJson)},
	})

	w.WriteHeader(http.StatusCreated)
}

func (s *Server) UpdateConfig(w http.ResponseWriter, r *http.Request, configName string) {
	var req ConfigMergePatch

	if err := DecodeRequestBody(r, &req); err != nil {
		log.Println("Error decoding request body: ", err)
		HandleAppError(w, err)
		return
	}

	if !utils.IsValidName(configName) {
		HandleAppError(w, &AppError{
			Message: "invalid or empty config_name",
			Code:    http.StatusBadRequest,
		})
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Printf("error starting transaction: %v", err)
		err := &AppError{
			Message: "error: failed to start database transaction",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}
	q := s.dbQueries.WithTx(tx)

	committed := false
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil {
				log.Printf("Error: Failed to rollback transaction for failed process: %v", err)
				err := &AppError{
					Message: "Error: Failed to rollback transaction",
					Code:    http.StatusInternalServerError,
				}
				HandleAppError(w, err)
				return
			}
		}
	}()

	existing, err := q.GetConfigByName(r.Context(), configName)
	if err != nil {
		// If config not found, send StatusNotFound
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("error: config not found : %s", configName)
			err := &AppError{
				Message: "error: config not found",
				Code:    http.StatusNotFound,
			}
			HandleAppError(w, err)
			return
		}
		// If any other errors send 500
		err := &AppError{
			Message: "error: failed to get config",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	configJson, err := json.Marshal(req)
	if err != nil {
		log.Println("Could not marshal JSON: ", err)
		HandleAppError(w, err)
		return
	}

	patchedJson, err := jsonpatch.MergePatch(existing.Config, configJson)
	if err != nil {
		log.Printf("error: unable to apply patch %v", err)
		err := &AppError{
			Message: "error: unable to apply patch",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	if err := ensureSatelliteProjectExists(r.Context()); err != nil {
		log.Println("Error while ensuring project satellite: ", err)
		HandleAppError(w, err)
		return
	}

	params := database.UpdateConfigParams{
		ConfigName:  configName,
		RegistryUrl: env.GC.Harbor.URL,
		Config:      patchedJson,
	}

	result, err := q.UpdateConfig(r.Context(), params)
	if err != nil {
		log.Println("Error persisting config object to database: ", err)
		HandleAppError(w, err)
		return
	}

	// Push config as OCI artifact
	err = utils.CreateAndPushConfigStateArtifact(r.Context(), patchedJson, configName)
	if err != nil {
		log.Println("Error while creating config state artifact: ", err)
		HandleAppError(w, err)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Failed to commit transaction: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Failed to commit transaction",
			Code:    http.StatusInternalServerError,
		})
		return
	}
	committed = true

	updateEvent := auditlog.AuditEvent{
		Operation:    auditlog.OpUpdate,
		ResourceType: auditlog.ResConfig,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        actorFromContext(r.Context()),
		ActorType:    auditlog.ActorUser,
		Resource:     configName,
	}
	// Record only the fields that changed (from -> to), with secret values
	// redacted. Computed from the raw configs so a rotated secret still shows up
	// as a changed path even though its value is not logged.
	if changed := diffConfigForAudit(existing.Config, patchedJson); len(changed) > 0 {
		updateEvent.Details = map[string]any{"changed": changed}
	}
	s.auditEvent(r, updateEvent)

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) ListConfigs(w http.ResponseWriter, r *http.Request) {
	result, err := s.dbQueries.ListConfigs(r.Context())
	if err != nil {
		fmt.Println("Could not list configs: ", err)
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) GetConfig(w http.ResponseWriter, r *http.Request, configName string) {
	result, err := s.dbQueries.GetConfigByName(r.Context(), configName)
	if err != nil {
		fmt.Println("Could not get config: ", err)
		HandleAppError(w, &AppError{
			Message: fmt.Sprintf("Config not found: %v", err),
			Code:    http.StatusNotFound,
		})
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) SetSatelliteConfig(w http.ResponseWriter, r *http.Request) {
	var req SatelliteConfigParams
	var err error

	if err := DecodeRequestBody(r, &req); err != nil {
		log.Println("Error decoding request body: ", err)
		HandleAppError(w, err)
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Println("Could not begin transaction:", err)
		HandleAppError(w, err)
		return
	}

	committed := false
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil {
				log.Printf("Error: Failed to rollback transaction for failed process: %v", err)
				err := &AppError{
					Message: "Error: Failed to rollback transaction",
					Code:    http.StatusInternalServerError,
				}
				HandleAppError(w, err)
				return
			}
		}
	}()

	q := s.dbQueries.WithTx(tx)

	sat, err := setSatelliteConfig(r.Context(), q, req.Satellite, req.ConfigName)
	if err != nil {
		log.Printf("Error: Could not set satellite config: %v", err)
		HandleAppError(w, err)
		return
	}

	groupList, err := q.SatelliteGroupList(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Could not get satellite group list: %v", err)
		err := &AppError{
			Message: "Error: Failed to Add satellite to config",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	// TODO: Store the groupStates in memory to survive hot reloads
	var groupStates []string
	for _, group := range groupList {
		grp, err := q.GetGroupByID(r.Context(), group.GroupID)
		if err != nil {
			log.Printf("Error: Failed: %v", err)
			err := &AppError{
				Message: "Error: Failed to Add satellite to config",
				Code:    http.StatusInternalServerError,
			}
			HandleAppError(w, err)
			return
		}
		groupStates = append(groupStates, utils.AssembleGroupState(grp.GroupName))
	}

	err = utils.CreateOrUpdateSatStateArtifact(r.Context(), sat.Name, groupStates, req.ConfigName)
	if err != nil {
		log.Printf("Could not update satellite state artifact: %v", err)
		HandleAppError(w, err)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Failed to commit transaction: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Failed to commit transaction",
			Code:    http.StatusInternalServerError,
		})
		return
	}
	committed = true

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}

// Deletes the config, given that the config is not currently used by any satellite.
func (s *Server) DeleteConfig(w http.ResponseWriter, r *http.Request, configName string) {
	q := s.dbQueries

	configObject, err := q.GetConfigByName(r.Context(), configName)
	if err != nil {
		log.Printf("Error: Failed to get Config: %v", err)
		err := &AppError{
			Message: "Error: Failed to get Config",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	isConfigInUse, err := isConfigInUse(r.Context(), q, configObject)
	if err != nil {
		log.Printf("Error: Could not delete config: %v", err)
		HandleAppError(w, err)
		return
	}

	if isConfigInUse {
		log.Printf("Cannot delete config that is in use")
		HandleAppError(w, &AppError{
			Message: "Cannot delete config that is in use",
			Code:    http.StatusBadRequest,
		})
		return
	}

	if err := utils.DeleteArtifact(utils.ConstructHarborDeleteURL(configName, "config")); err != nil {
		log.Printf("Could not delete config state artifact: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Could not delete config state artifact",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	if err := q.DeleteConfig(r.Context(), configObject.ID); err != nil {
		log.Println("Error: Could not delete config: ", err)
		HandleAppError(w, err)
		return
	}

	s.auditEvent(r, auditlog.AuditEvent{
		Operation:    auditlog.OpDelete,
		ResourceType: auditlog.ResConfig,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        actorFromContext(r.Context()),
		ActorType:    auditlog.ActorUser,
		Resource:     configName,
		Details:      map[string]any{"from": redactConfigForAudit(configObject.Config)},
	})

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}
