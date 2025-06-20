package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
	"github.com/container-registry/harbor-satellite/ground-control/internal/models"
	"github.com/container-registry/harbor-satellite/ground-control/internal/utils"
	"github.com/container-registry/harbor-satellite/pkg/config"
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/gorilla/mux"
	"github.com/lib/pq"
)

type SatelliteConfigParams struct {
	Satellite  string `json:"satellite,omitempty"`
	ConfigName string `json:"config_name"`
}

func (s *Server) createConfigHandler(w http.ResponseWriter, r *http.Request) {
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
		RegistryUrl: os.Getenv("HARBOR_URL"),
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

	w.WriteHeader(http.StatusCreated)
}

func (s *Server) updateConfigHandler(w http.ResponseWriter, r *http.Request) {
	var req config.Config

	vars := mux.Vars(r)
	configName := vars["config"]

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
		//If config not found, send StatusNotFound
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
		RegistryUrl: os.Getenv("HARBOR_URL"),
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

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) listConfigsHandler(w http.ResponseWriter, r *http.Request) {
	result, err := s.dbQueries.ListConfigs(r.Context())
	if err != nil {
		fmt.Println("Could not list configs: ", err)
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) getConfigHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	configName := vars["config"]

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

func (s *Server) setSatelliteConfig(w http.ResponseWriter, r *http.Request) {
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
func (s *Server) deleteConfigHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	configName := vars["config"]

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

	if err := utils.DeleteArtifact(utils.ConstructHarborDeleteURL(fmt.Sprintf("config-state/%s/state", configName))); err != nil {
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

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}
