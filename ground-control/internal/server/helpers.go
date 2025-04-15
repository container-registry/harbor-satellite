package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"github.com/container-registry/harbor-satellite/ground-control/reg/harbor"

	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
	"github.com/container-registry/harbor-satellite/ground-control/internal/models"
	"github.com/robfig/cron/v3"
)

func updateSatelliteConfig(ctx context.Context, q *database.Queries, satelliteName string, configName string) (*database.Satellite, error) {
	sat, err := q.GetSatelliteByName(ctx, satelliteName)
	if err != nil {
		log.Printf("Error: Satellite Not Found: %v", err)
		return nil, &AppError{
			Message: "Error: Satellite Not Found",
			Code:    http.StatusBadRequest,
		}
	}

	configObject, err := q.GetConfigByName(ctx, configName)
	if err != nil {
		log.Printf("Error: Config Not Found: %v", err)
		return nil, &AppError{
			Message: "Error: Config Not Found",
			Code:    http.StatusBadRequest,
		}
	}

	params := database.SetSatelliteConfigParams{
		SatelliteID: int32(sat.ID),
		ConfigID:    int32(configObject.ID),
	}

	err = q.SetSatelliteConfig(ctx, params)
	if err != nil {
		log.Printf("Error: Failed to Set Satellite Config: %v", err)
		return nil, &AppError{
			Message: "Error: Failed to Set Satellite config",
			Code:    http.StatusInternalServerError,
		}
	}
	return &sat, nil
}

// check if project satellite exists and if does not exist create project satellite
func ensureSatelliteProjectExists(ctx context.Context) error {
	satExist, err := harbor.GetProject(ctx, "satellite")
	if err != nil {
		log.Println(err)
		return &AppError{
			Message: fmt.Sprintf("Error: Checking satellite project: %v", err),
			Code:    http.StatusBadGateway,
		}
	}
	if !satExist {
		_, err := harbor.CreateSatelliteProject(ctx)
		if err != nil {
			log.Println(err)
			return &AppError{
				Message: fmt.Sprintf("Error: creating satellite project: %v", err),
				Code:    http.StatusBadGateway,
			}
		}
	}
	return nil
}

func isConfigInUse(ctx context.Context, q *database.Queries, configObject database.Config) error {
	configSatellites, err := q.ConfigSatelliteList(ctx, configObject.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Printf("Failed to get the list of Satellites that use this Config: %v", err)
		err := &AppError{
			Message: "Error: Failed to get Satellites that use this Config",
			Code:    http.StatusInternalServerError,
		}
		return err
	}

	if len(configSatellites) > 0 {
		log.Println("Cannot delete config %s: it is currently in use", configObject.ConfigName)
		err := &AppError{
			Message: "Cannot delete a config that is currently in use",
			Code:    http.StatusInternalServerError,
		}
		return err
	}

	return nil
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
