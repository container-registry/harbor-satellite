package server

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
	"github.com/container-registry/harbor-satellite/ground-control/internal/models"
	"github.com/container-registry/harbor-satellite/ground-control/internal/utils"
	"github.com/gorilla/mux"
)

type SatelliteConfigParams struct {
	Satellite  string `json:"satellite,omitempty"`
	ConfigName string `json:"config_name"`
}

func (s *Server) configsSyncHandler(w http.ResponseWriter, r *http.Request) {
	var req models.ConfigObject
	var err error

	if err := DecodeRequestBody(r, &req); err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	q := s.dbQueries.WithTx(tx)

	configJson, err := json.Marshal(req.Config)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	params := database.CreateConfigParams{
		ConfigName:  req.ConfigName,
		RegistryUrl: os.Getenv("HARBOR_URL"),
		Config:      configJson,
	}

	result, err := q.CreateConfig(r.Context(), params)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	if err := ensureSatelliteProjectExists(r.Context()); err != nil {
		HandleAppError(w, err)
		tx.Rollback()
		return
	}

	// Upload config as OCI artifact
	err = utils.CreateConfigStateArtifact(r.Context(), &req)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Commit failed: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Could not commit transaction",
			Code:    http.StatusInternalServerError,
		})
		return
	}
	committed = true
	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) listConfigsHandler(w http.ResponseWriter, r *http.Request) {
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	q := s.dbQueries.WithTx(tx)

	result, err := q.ListConfigs(r.Context())
	if err != nil {
		HandleAppError(w, err)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Commit failed: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Could not commit transaction",
			Code:    http.StatusInternalServerError,
		})
		return
	}
	committed = true
	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) getConfigHandler(w http.ResponseWriter, r *http.Request) {
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	q := s.dbQueries.WithTx(tx)

	vars := mux.Vars(r)
	configName := vars["config"]

	result, err := q.GetConfigByName(r.Context(), configName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Commit failed: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Could not commit transaction",
			Code:    http.StatusInternalServerError,
		})
		return
	}
	committed = true
	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) setSatelliteConfig(w http.ResponseWriter, r *http.Request) {
	var req SatelliteConfigParams
	var err error

	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	q := s.dbQueries.WithTx(tx)

	sat, err := updateSatelliteConfig(r.Context(), q, req.Satellite, req.ConfigName)
	if err != nil {
		log.Printf("Error: Could not set satellite config: %v", err)
		HandleAppError(w, err)
		return
	}

	groupList, err := q.SatelliteGroupList(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed: %v", err)
		err := &AppError{
			Message: "Error: Failed to Add satellite to config",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	// TODO: maybe we should store the current list of states in the DB?
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

	err = utils.CreateOrUpdateSatStateArtifact(r.Context(), sat.Name, groupStates, utils.AssembleConfigState(req.ConfigName))
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Commit failed: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Could not commit transaction",
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

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	q := s.dbQueries.WithTx(tx)

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

	if err := isConfigInUse(r.Context(), q, configObject); err != nil {
		log.Printf("Error: Could not delete config: %v", err)
		HandleAppError(w, err)
		return
	}

	if err := q.DeleteConfig(r.Context(), configObject.ID); err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	if err := utils.DeleteConfigStateArtifact(configName); err != nil {
		log.Printf("Could not delete config state artifact: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Could not delete config state artifact",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Commit failed: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Could not commit transaction",
			Code:    http.StatusInternalServerError,
		})
		return
	}
	committed = true

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}
