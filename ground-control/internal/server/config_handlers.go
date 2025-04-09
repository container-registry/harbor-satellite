package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
	"github.com/container-registry/harbor-satellite/ground-control/internal/models"
	"github.com/container-registry/harbor-satellite/ground-control/internal/utils"
	"github.com/container-registry/harbor-satellite/ground-control/reg/harbor"
	"github.com/gorilla/mux"
	"github.com/robfig/cron/v3"
)

type SatelliteConfigParams struct {
	Satellite  string `json:"satellite,omitempty"`
	ConfigName string `json:"config_name,omitempty"`
}

// validateCronExpression checks the validity of a cron expression.
func isValidCronExpression(cronExpression string) bool {
	if _, err := cron.ParseStandard(cronExpression); err != nil {
		return false
	}
	return true
}

func isValidConfig(config models.SatelliteConfig) error {
	if _, err := url.Parse(config.GroundControlURL); err != nil {
		return fmt.Errorf("The provided ground_control_url %s is invalid", config.GroundControlURL)
	}

	if _, err := url.Parse(config.LocalRegistryConfig.URL); err != nil {
		return fmt.Errorf("The provided local_registry.url %s is invalid", config.LocalRegistryConfig.URL)
	}

	if !isValidCronExpression(config.UpdateConfigInterval) {
		return fmt.Errorf("The provided update_config_interval %s is not a valid cron expression")
	}

	if !isValidCronExpression(config.StateReplicationInterval) {
		return fmt.Errorf("The provided state_replication_interval %s is not a valid cron expression")
	}

	if !isValidCronExpression(config.RegisterSatelliteInterval) {
		return fmt.Errorf("The provided register_satellite_interval %s is not a valid cron expression")
	}
	return nil
}

func (s *Server) configsSyncHandler(w http.ResponseWriter, r *http.Request) {
	var req models.ConfigObject
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
	q := s.dbQueries.WithTx(tx)
	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
		} else if err != nil {
			tx.Rollback()
		}
	}()

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

	satExist, err := harbor.GetProject(r.Context(), "satellite")
	if err != nil {
		err := &AppError{
			Message: fmt.Sprintf("Error: Checking satellite project: %v", err),
			Code:    http.StatusBadGateway,
		}
		log.Println(err)
		HandleAppError(w, err)
		tx.Rollback()
		return
	}
	if !satExist {
		_, err := harbor.CreateSatelliteProject(r.Context())
		if err != nil {
			err := &AppError{
				Message: fmt.Sprintf("Error: creating satellite project: %v", err),
				Code:    http.StatusBadGateway,
			}
			log.Println(err)
			HandleAppError(w, err)
			tx.Rollback()
			return
		}
	}

	// Upload config as OCI artifact
	err = utils.CreateConfigStateArtifact(&req)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	tx.Commit()
	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) listConfigsHandler(w http.ResponseWriter, r *http.Request) {
	result, err := s.dbQueries.ListConfigs(r.Context())
	if err != nil {
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
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) addSatelliteToConfig(w http.ResponseWriter, r *http.Request) {
	var req SatelliteConfigParams
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	sat, err := s.dbQueries.GetSatelliteByName(r.Context(), req.Satellite)
	if err != nil {
		log.Printf("Error: Satellite Not Found: %v", err)
		err := &AppError{
			Message: "Error: Satellite Not Found",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}
	configObject, err := s.dbQueries.GetConfigByName(r.Context(), req.ConfigName)
	if err != nil {
		log.Printf("Error: Config Not Found: %v", err)
		err := &AppError{
			Message: "Error: Config Not Found",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	params := database.SetSatelliteConfigParams{
		SatelliteID: int32(sat.ID),
		ConfigID:    int32(configObject.ID),
	}

	err = s.dbQueries.SetSatelliteConfig(r.Context(), params)
	if err != nil {
		log.Printf("Error: Failed to Set Satellite Config: %v", err)
		err := &AppError{
			Message: "Error: Failed to Set Satellite Group",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	groupList, err := s.dbQueries.SatelliteGroupList(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed: %v", err)
		err := &AppError{
			Message: "Error: Failed to Add satellite to group",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	// TODO: maybe we should store the current list of states in the DB?
	var groupStates []string
	for _, group := range groupList {
		grp, err := s.dbQueries.GetGroupByID(r.Context(), group.GroupID)
		if err != nil {
			log.Printf("Error: Failed: %v", err)
			err := &AppError{
				Message: "Error: Failed to Add satellite to group",
				Code:    http.StatusInternalServerError,
			}
			HandleAppError(w, err)
			return
		}
		groupStates = append(groupStates, utils.AssembleGroupState(grp.GroupName))
	}

	err = utils.CreateOrUpdateSatStateArtifact(sat.Name, groupStates, utils.AssembleConfigState(configObject.ConfigName))
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}
