package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/container-registry/harbor-satellite/internal/registry"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
)

// ValidateAndEnforceDefaults validates and normalizes the given config.
// It applies default values where required, verifies URLs, cron expressions,
// and handles the logic for bring-your-own-registry vs default registry setup.
// Returns warnings for any defaulted or ignored fields and a fatal error for critical misconfigurations.
func ValidateAndEnforceDefaults(config *Config, defaultGroundControlURL string) (*Config, []string, error) {
	if config == nil {
		config = &Config{}
	}

	var warnings []string

	if config.AppConfig.GroundControlURL == "" {
		warnings = append(warnings, fmt.Sprintf(
			"ground_control_url not provided. Defaulting to default ground_control_url: %s", defaultGroundControlURL,
		))
		config.AppConfig.GroundControlURL = URL(defaultGroundControlURL)
	}

	if _, err := url.ParseRequestURI(string(config.AppConfig.GroundControlURL)); err != nil {
		return nil, nil, fmt.Errorf("invalid URL provided for ground_control_url: %w", err)
	}

	if config.AppConfig.LogLevel == "" {
		config.AppConfig.LogLevel = zerolog.LevelInfoValue
	} else if !validLogLevels[strings.ToLower(config.AppConfig.LogLevel)] {
		warnings = append(warnings, fmt.Sprintf(
			"invalid log_level '%s' provided. Valid options are: info, debug, panic, error, warn, fatal. Defaulting to 'info'.",
			config.AppConfig.LogLevel,
		))
		config.AppConfig.LogLevel = zerolog.LevelInfoValue
	}

	bringOwnRegistry := config.AppConfig.BringOwnRegistry

	if bringOwnRegistry {
		if config.AppConfig.LocalRegistryCredentials.URL == "" {
			return nil, nil, fmt.Errorf("custom registry URL is required when BringOwnRegistry is enabled")
		}

		if _, err := url.ParseRequestURI(string(config.AppConfig.LocalRegistryCredentials.URL)); err != nil {
			return nil, nil, fmt.Errorf("invalid custom registry URL: %w", err)
		}

		if config.AppConfig.LocalRegistryCredentials.Username == "" || config.AppConfig.LocalRegistryCredentials.Password == "" {
			warnings = append(warnings, "username or password for custom registry is empty.")
		}

		if len(config.ZotConfigRaw) > 0 {
			warnings = append(warnings,
				"redundant zot_config provided for bring_own_registry: `true`.",
			)
			config.ZotConfigRaw = json.RawMessage{}
		}
	}

	if !bringOwnRegistry && len(config.ZotConfigRaw) == 0 {
		warnings = append(warnings, fmt.Sprintf(
			"empty zot_config provided. Defaulting to: %v", DefaultZotConfigJSON,
		))
		config.ZotConfigRaw = json.RawMessage(DefaultZotConfigJSON)
	}

	var zotConfig registry.ZotConfig
	if err := json.Unmarshal(config.ZotConfigRaw, &zotConfig); err != nil {
		return nil, nil, fmt.Errorf("invalid zot_config: %w", err)
	}

	if !bringOwnRegistry && config.AppConfig.LocalRegistryCredentials.URL == "" {
		warnings = append(warnings, fmt.Sprintf(
			"remote registry URL is empty. Defaulting to value from zot_config %s",
			DefaultRemoteRegistryURL,
		))
		config.AppConfig.LocalRegistryCredentials.URL = URL(zotConfig.GetRegistryURL())
	}

	if config.HeartbeatConfig.StateReportInterval == "" {
		config.HeartbeatConfig.StateReportInterval = DefaultStateReportCronExpr
		warnings = append(warnings, fmt.Sprintf("heartbeat interval not specified, defaulting to : %s", DefaultStateReportCronExpr))
	}

	if !isValidCronExpression(config.AppConfig.StateReplicationInterval) {
		config.AppConfig.StateReplicationInterval = DefaultFetchAndReplicateCronExpr
		warnings = append(warnings, fmt.Sprintf("invalid schedule provided for state_replication_interval, using default schedule %s", DefaultFetchAndReplicateCronExpr))
	}

	if !isValidCronExpression(config.AppConfig.RegisterSatelliteInterval) {
		config.AppConfig.RegisterSatelliteInterval = DefaultZTRCronExpr
		warnings = append(warnings, fmt.Sprintf("invalid schedule provided for register_satellite_interval, using default schedule %s", DefaultZTRCronExpr))
	}

	if !isValidCronExpression(config.HeartbeatConfig.StateReportInterval) {
		config.HeartbeatConfig.StateReportInterval = DefaultStateReportCronExpr
		warnings = append(warnings, fmt.Sprintf("invalid schedule provided for state_report_interval, using default schedule %s", DefaultStateReportCronExpr))
	}

	return config, warnings, nil
}

// validateCronExpression checks the validity of a cron expression.
func isValidCronExpression(cronExpression string) bool {
	if _, err := cron.ParseStandard(cronExpression); err != nil {
		return false
	}
	return true
}
