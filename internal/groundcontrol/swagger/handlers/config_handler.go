package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/env"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbor"
	auditlog "github.com/container-registry/harbor-satellite/internal/groundcontrol/logger"
	swaggermodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/models"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/configs"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/utils"
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/go-openapi/runtime/middleware"
	"github.com/lib/pq"
)

const maxConfigPatchBytes = 10 << 20

func CreateConfig(params configs.CreateConfigParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return configs.NewCreateConfigInternalServerError().WithPayload(internalError("Failed to initialize configuration service", err))
	}
	actor, errPayload := requirePrincipal(principal)
	if errPayload != nil {
		return configs.NewCreateConfigUnauthorized().WithPayload(errPayload)
	}
	if params.Body == nil {
		return configs.NewCreateConfigBadRequest().WithPayload(appError("Invalid request body", http.StatusBadRequest))
	}
	if !utils.IsValidName(params.Body.ConfigName) {
		return configs.NewCreateConfigBadRequest().WithPayload(appError("invalid or empty config_name", http.StatusBadRequest))
	}

	ctx := params.HTTPRequest.Context()
	tx, err := svc.db.BeginTx(ctx, nil)
	if err != nil {
		return configs.NewCreateConfigInternalServerError().WithPayload(internalError("Failed to start configuration creation transaction", err))
	}
	q := svc.queries.WithTx(tx)
	committed := false
	defer rollbackUnlessCommitted(tx, &committed)

	configJSON, err := json.Marshal(params.Body.Config)
	if err != nil {
		return configs.NewCreateConfigBadRequest().WithPayload(appError("Invalid config payload", http.StatusBadRequest))
	}

	if err := ensureSatelliteProjectExists(ctx); err != nil {
		return configs.NewCreateConfigInternalServerError().WithPayload(internalError("Failed to ensure the Harbor satellite project exists", err))
	}

	_, err = q.CreateConfig(ctx, database.CreateConfigParams{
		ConfigName:  params.Body.ConfigName,
		RegistryUrl: env.GC.Harbor.URL,
		Config:      configJSON,
	})
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return configs.NewCreateConfigConflict().WithPayload(appError("error: config already exists", http.StatusConflict))
		}
		return configs.NewCreateConfigInternalServerError().WithPayload(internalError("Failed to create configuration record", err))
	}

	if err := utils.CreateAndPushConfigStateArtifact(ctx, configJSON, params.Body.ConfigName); err != nil {
		return configs.NewCreateConfigInternalServerError().WithPayload(internalError("Failed to create or push configuration state artifact", err))
	}

	if err := tx.Commit(); err != nil {
		return configs.NewCreateConfigInternalServerError().WithPayload(internalError("Failed to commit configuration creation transaction", err))
	}
	committed = true
	svc.auditEvent(params.HTTPRequest, auditlog.AuditEvent{
		Operation:    auditlog.OpCreate,
		ResourceType: auditlog.ResConfig,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        actor.Username,
		ActorType:    auditlog.ActorUser,
		Resource:     params.Body.ConfigName,
		Details:      map[string]any{"to": auditlog.RedactConfig(configJSON)},
	})

	return configs.NewCreateConfigCreated()
}

func DeleteConfig(params configs.DeleteConfigParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return configs.NewDeleteConfigInternalServerError().WithPayload(internalError("Failed to initialize configuration service", err))
	}
	actor, errPayload := requirePrincipal(principal)
	if errPayload != nil {
		return configs.NewDeleteConfigUnauthorized().WithPayload(errPayload)
	}

	ctx := params.HTTPRequest.Context()
	configObject, err := svc.queries.GetConfigByName(ctx, params.Config)
	if err != nil {
		return configs.NewDeleteConfigInternalServerError().WithPayload(internalError("Failed to load configuration for deletion", err))
	}

	inUse, err := isConfigInUse(ctx, svc.queries, configObject)
	if err != nil {
		return configs.NewDeleteConfigInternalServerError().WithPayload(internalError("Failed to check whether configuration is in use", err))
	}
	if inUse {
		return configs.NewDeleteConfigBadRequest().WithPayload(appError("Cannot delete config that is in use", http.StatusBadRequest))
	}

	if err := utils.DeleteArtifact(utils.ConstructHarborDeleteURL(params.Config, "config")); err != nil {
		return configs.NewDeleteConfigInternalServerError().WithPayload(internalError("Failed to delete configuration state artifact from Harbor", err))
	}

	if err := svc.queries.DeleteConfig(ctx, configObject.ID); err != nil {
		return configs.NewDeleteConfigInternalServerError().WithPayload(internalError("Failed to delete configuration record", err))
	}
	svc.auditEvent(params.HTTPRequest, auditlog.AuditEvent{
		Operation:    auditlog.OpDelete,
		ResourceType: auditlog.ResConfig,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        actor.Username,
		ActorType:    auditlog.ActorUser,
		Resource:     params.Config,
		Details:      map[string]any{"from": auditlog.RedactConfig(configObject.Config)},
	})

	return configs.NewDeleteConfigOK().WithPayload(map[string]string{})
}

func GetConfig(params configs.GetConfigParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return configs.NewGetConfigInternalServerError().WithPayload(internalError("Failed to initialize configuration service", err))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return configs.NewGetConfigUnauthorized().WithPayload(errPayload)
	}

	config, err := svc.queries.GetConfigByName(params.HTTPRequest.Context(), params.Config)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return configs.NewGetConfigNotFound().WithPayload(appError("Config not found", http.StatusNotFound))
		}
		return configs.NewGetConfigInternalServerError().WithPayload(internalError("Failed to load configuration", err))
	}

	return configs.NewGetConfigOK().WithPayload(databaseConfig(config))
}

func ListConfigs(params configs.ListConfigsParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return configs.NewListConfigsInternalServerError().WithPayload(internalError("Failed to initialize configuration service", err))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return configs.NewListConfigsUnauthorized().WithPayload(errPayload)
	}

	rows, err := svc.queries.ListConfigs(params.HTTPRequest.Context())
	if err != nil {
		return configs.NewListConfigsInternalServerError().WithPayload(internalError("Failed to list configurations", err))
	}

	response := make([]*swaggermodels.APIDatabaseConfig, 0, len(rows))
	for _, row := range rows {
		response = append(response, databaseConfig(row))
	}

	return configs.NewListConfigsOK().WithPayload(response)
}

func SetSatelliteConfig(params configs.SetSatelliteConfigParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return configs.NewSetSatelliteConfigInternalServerError().WithPayload(internalError("Failed to initialize configuration service", err))
	}
	actor, errPayload := requirePrincipal(principal)
	if errPayload != nil {
		return configs.NewSetSatelliteConfigUnauthorized().WithPayload(errPayload)
	}
	if params.Body == nil {
		return configs.NewSetSatelliteConfigBadRequest().WithPayload(appError("Invalid request body", http.StatusBadRequest))
	}

	ctx := params.HTTPRequest.Context()
	tx, err := svc.db.BeginTx(ctx, nil)
	if err != nil {
		return configs.NewSetSatelliteConfigInternalServerError().WithPayload(internalError("Failed to start satellite configuration transaction", err))
	}
	q := svc.queries.WithTx(tx)
	committed := false
	defer rollbackUnlessCommitted(tx, &committed)

	sat, errPayload := setSatelliteConfig(ctx, q, params.Body.Satellite, params.Body.ConfigName)
	if errPayload != nil {
		return configs.NewSetSatelliteConfigBadRequest().WithPayload(errPayload)
	}

	groupList, err := q.SatelliteGroupList(ctx, sat.ID)
	if err != nil {
		return configs.NewSetSatelliteConfigInternalServerError().WithPayload(internalError("Failed to load satellite group membership", err))
	}

	groupStates := make([]string, 0, len(groupList))
	for _, group := range groupList {
		grp, err := q.GetGroupByID(ctx, group.GroupID)
		if err != nil {
			return configs.NewSetSatelliteConfigInternalServerError().WithPayload(internalError("Failed to load satellite group", err))
		}
		groupStates = append(groupStates, utils.AssembleGroupState(grp.GroupName))
	}

	if err := utils.CreateOrUpdateSatStateArtifact(ctx, sat.Name, groupStates, params.Body.ConfigName); err != nil {
		return configs.NewSetSatelliteConfigInternalServerError().WithPayload(internalError("Failed to update satellite state artifact", err))
	}

	if err := tx.Commit(); err != nil {
		return configs.NewSetSatelliteConfigInternalServerError().WithPayload(internalError("Failed to commit satellite configuration transaction", err))
	}
	committed = true
	svc.auditEvent(params.HTTPRequest, auditlog.AuditEvent{
		Operation:    auditlog.OpUpdate,
		ResourceType: auditlog.ResConfig,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        actor.Username,
		ActorType:    auditlog.ActorUser,
		SatelliteID:  sat.Name,
		Resource:     params.Body.ConfigName,
		Details:      map[string]any{"assignment": "satellite"},
	})

	return configs.NewSetSatelliteConfigOK().WithPayload(map[string]string{})
}

func UpdateConfig(params configs.UpdateConfigParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return configs.NewUpdateConfigInternalServerError().WithPayload(internalError("Failed to initialize configuration service", err))
	}
	actor, errPayload := requirePrincipal(principal)
	if errPayload != nil {
		return configs.NewUpdateConfigUnauthorized().WithPayload(errPayload)
	}
	if params.Body == nil {
		return configs.NewUpdateConfigBadRequest().WithPayload(appError("Invalid request body", http.StatusBadRequest))
	}
	if !utils.IsValidName(params.Config) {
		return configs.NewUpdateConfigBadRequest().WithPayload(appError("invalid or empty config_name", http.StatusBadRequest))
	}

	ctx := params.HTTPRequest.Context()
	tx, err := svc.db.BeginTx(ctx, nil)
	if err != nil {
		return configs.NewUpdateConfigInternalServerError().WithPayload(internalError("Failed to start configuration update transaction", err))
	}
	q := svc.queries.WithTx(tx)
	committed := false
	defer rollbackUnlessCommitted(tx, &committed)

	existing, err := q.GetConfigByName(ctx, params.Config)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return configs.NewUpdateConfigNotFound().WithPayload(appError("error: config not found", http.StatusNotFound))
		}
		return configs.NewUpdateConfigInternalServerError().WithPayload(internalError("Failed to load configuration for update", err))
	}

	configJSON, err := configPatchJSON(params.HTTPRequest, params.Body)
	if err != nil {
		return configs.NewUpdateConfigBadRequest().WithPayload(appError("Invalid config payload", http.StatusBadRequest))
	}

	patchedJSON, err := jsonpatch.MergePatch(existing.Config, configJSON)
	if err != nil {
		return configs.NewUpdateConfigBadRequest().WithPayload(appError("error: unable to apply patch", http.StatusBadRequest))
	}

	if err := ensureSatelliteProjectExists(ctx); err != nil {
		return configs.NewUpdateConfigInternalServerError().WithPayload(internalError("Failed to ensure the Harbor satellite project exists", err))
	}

	result, err := q.UpdateConfig(ctx, database.UpdateConfigParams{
		ConfigName:  params.Config,
		RegistryUrl: env.GC.Harbor.URL,
		Config:      patchedJSON,
	})
	if err != nil {
		return configs.NewUpdateConfigInternalServerError().WithPayload(internalError("Failed to update configuration record", err))
	}

	if err := utils.CreateAndPushConfigStateArtifact(ctx, patchedJSON, params.Config); err != nil {
		return configs.NewUpdateConfigInternalServerError().WithPayload(internalError("Failed to create or push updated configuration state artifact", err))
	}

	if err := tx.Commit(); err != nil {
		return configs.NewUpdateConfigInternalServerError().WithPayload(internalError("Failed to commit configuration update transaction", err))
	}
	committed = true
	updateEvent := auditlog.AuditEvent{
		Operation:    auditlog.OpUpdate,
		ResourceType: auditlog.ResConfig,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        actor.Username,
		ActorType:    auditlog.ActorUser,
		Resource:     params.Config,
	}
	if changed := auditlog.DiffConfig(existing.Config, patchedJSON); len(changed) > 0 {
		updateEvent.Details = map[string]any{"changed": changed}
	}
	svc.auditEvent(params.HTTPRequest, updateEvent)

	return configs.NewUpdateConfigOK().WithPayload(databaseConfig(result))
}

type configPatchContextKey struct{}

type capturedConfigPatch struct {
	body []byte
	err  error
}

// CaptureConfigPatchBody preserves the original merge-patch document before
// go-swagger decodes it into a generated model. The generated model cannot
// distinguish an omitted property from an explicit JSON null.
func CaptureConfigPatchBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil {
			next.ServeHTTP(w, r)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, maxConfigPatchBytes+1))
		_ = r.Body.Close()
		if len(body) > maxConfigPatchBytes {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			if encodeErr := json.NewEncoder(w).Encode(appError("Configuration merge patch exceeds the 10 MiB limit", http.StatusRequestEntityTooLarge)); encodeErr != nil {
				log.Printf("failed to write oversized configuration patch response: %v", encodeErr)
			}
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		capture := capturedConfigPatch{body: body, err: err}
		ctx := context.WithValue(r.Context(), configPatchContextKey{}, capture)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func configPatchJSON(r *http.Request, body *swaggermodels.APIConfigValue) ([]byte, error) {
	if r == nil {
		return json.Marshal(body)
	}
	capture, captured := r.Context().Value(configPatchContextKey{}).(capturedConfigPatch)
	if !captured {
		return json.Marshal(body)
	}
	if capture.err != nil {
		return nil, fmt.Errorf("read config merge patch: %w", capture.err)
	}
	if err := validateConfigPatch(capture.body); err != nil {
		return nil, err
	}
	return capture.body, nil
}

func validateConfigPatch(patch []byte) error {
	var sections map[string]json.RawMessage
	if err := json.Unmarshal(patch, &sections); err != nil {
		return fmt.Errorf("invalid config merge patch: %w", err)
	}
	if sections == nil {
		return fmt.Errorf("invalid config merge patch: expected an object")
	}

	for section, value := range sections {
		switch section {
		case "app_config", "state_config", "zot_config":
		default:
			return fmt.Errorf("invalid config merge patch: unknown section %q", section)
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			continue
		}
		var object map[string]any
		if err := json.Unmarshal(value, &object); err != nil || object == nil {
			return fmt.Errorf("invalid config merge patch: section %q must be an object or null", section)
		}
	}
	return nil
}

func rollbackUnlessCommitted(tx *sql.Tx, committed *bool) {
	if *committed {
		return
	}
	if err := tx.Rollback(); err != nil {
		log.Printf("Error: Failed to rollback transaction for failed process: %v", err)
	}
}

func isConfigInUse(ctx context.Context, q *database.Queries, config database.Config) (bool, error) {
	satellites, err := q.ConfigSatelliteList(ctx, config.ID)
	if err != nil {
		return false, err
	}
	return len(satellites) > 0, nil
}

func setSatelliteConfig(ctx context.Context, q *database.Queries, satelliteName string, configName string) (*database.Satellite, *swaggermodels.AppError) {
	sat, err := q.GetSatelliteByName(ctx, satelliteName)
	if err != nil {
		return nil, appError("Error: Satellite Not Found", http.StatusBadRequest)
	}

	configObject, err := q.GetConfigByName(ctx, configName)
	if err != nil {
		return nil, appError("Error: Config Not Found", http.StatusBadRequest)
	}

	err = q.SetSatelliteConfig(ctx, database.SetSatelliteConfigParams{
		SatelliteID: sat.ID,
		ConfigID:    configObject.ID,
	})
	if err != nil {
		return nil, internalError("Failed to assign configuration to satellite", err)
	}

	return &sat, nil
}

func ensureSatelliteProjectExists(ctx context.Context) error {
	satExist, err := harbor.GetProject(ctx, "satellite")
	if err != nil {
		return err
	}
	if satExist {
		return nil
	}
	_, err = harbor.CreateSatelliteProject(ctx)
	return err
}
