package config

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
)

func validateConfig(config *Config) ([]string, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	var warnings []string

	if _, err := url.ParseRequestURI(string(config.AppConfig.GroundControlURL)); err != nil {
		return nil, fmt.Errorf("invalid URL provided for ground_control_url: %w", err)
	}

	if len(config.ZotConfigRaw) == 0 {
		return nil, fmt.Errorf("invalid zot_config. zot_config cannot be empty")
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

	if !isValidCronExpression(config.AppConfig.StateReplicationInterval) {
		config.AppConfig.StateReplicationInterval = DefaultFetchAndReplicateCronExpr
		warnings = append(warnings, fmt.Sprintf("invalid schedule provided for StateReplicationInterval, using default schedule %s", DefaultFetchAndReplicateCronExpr))
	}

	if !isValidCronExpression(config.AppConfig.RegisterSatelliteInterval) {
		config.AppConfig.RegisterSatelliteInterval = DefaultZTRCronExpr
		warnings = append(warnings, fmt.Sprintf("invalid schedule provided for StateReplicationInterval, using default schedule %s", DefaultZTRCronExpr))
	}

	if !isValidCronExpression(config.AppConfig.UpdateConfigInterval) {
		config.AppConfig.UpdateConfigInterval = DefaultFetchConfigCronExpr
		warnings = append(warnings, fmt.Sprintf("invalid schedule provided for StateReplicationInterval, using default schedule %s", DefaultFetchConfigCronExpr))
	}

	return warnings, nil
}

// validateCronExpression checks the validity of a cron expression.
func isValidCronExpression(cronExpression string) bool {
	if _, err := cron.ParseStandard(cronExpression); err != nil {
		return false
	}
	return true
}
