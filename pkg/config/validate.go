package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
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

	// Environment variable/CLI flag always takes precedence over config file
	if defaultGroundControlURL != "" {
		if config.AppConfig.GroundControlURL != "" && string(config.AppConfig.GroundControlURL) != defaultGroundControlURL {
			warnings = append(warnings, fmt.Sprintf(
				"ground_control_url from env/CLI (%s) takes precedence over config file (%s)",
				defaultGroundControlURL, config.AppConfig.GroundControlURL,
			))
		}
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

	// Check for USE_UNSECURE environment variable
	if useUnsecure := os.Getenv("USE_UNSECURE"); useUnsecure != "" {
		config.AppConfig.UseUnsecure = strings.ToLower(useUnsecure) == "true" || useUnsecure == "1"
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

	if !bringOwnRegistry {
		needsDefault := len(config.ZotConfigRaw) == 0 || strings.TrimSpace(string(config.ZotConfigRaw)) == "{}"
		if needsDefault {
			warnings = append(warnings, fmt.Sprintf(
				"empty zot_config provided. Defaulting to: %v", DefaultZotConfigJSON,
			))
			config.ZotConfigRaw = json.RawMessage(DefaultZotConfigJSON)
		}
	}

	var zotConfig registry.ZotConfig
	if err := json.Unmarshal(config.ZotConfigRaw, &zotConfig); err != nil {
		return nil, nil, fmt.Errorf("invalid zot_config: %w", err)
	}

	if !bringOwnRegistry && (zotConfig.HTTP.Address == "" || zotConfig.HTTP.Port == "") {
		warnings = append(warnings, "zot_config missing required http address/port. Applying defaults.")
		config.ZotConfigRaw = json.RawMessage(DefaultZotConfigJSON)
		if err := json.Unmarshal(config.ZotConfigRaw, &zotConfig); err != nil {
			return nil, nil, fmt.Errorf("invalid default zot_config: %w", err)
		}
	}

	if !bringOwnRegistry && config.AppConfig.LocalRegistryCredentials.URL == "" {
		warnings = append(warnings, fmt.Sprintf(
			"remote registry URL is empty. Defaulting to value from zot_config %s",
			DefaultRemoteRegistryURL,
		))
		config.AppConfig.LocalRegistryCredentials.URL = URL(zotConfig.GetRegistryURL())
	}

	if !isValidCronExpression(config.AppConfig.StateReplicationInterval) {
		config.AppConfig.StateReplicationInterval = DefaultFetchAndReplicateCronExpr
		warnings = append(warnings, fmt.Sprintf("invalid schedule provided for state_replication_interval, using default schedule %s", DefaultFetchAndReplicateCronExpr))
	}

	if !isValidCronExpression(config.AppConfig.RegisterSatelliteInterval) {
		config.AppConfig.RegisterSatelliteInterval = DefaultZTRCronExpr
		warnings = append(warnings, fmt.Sprintf("invalid schedule provided for register_satellite_interval, using default schedule %s", DefaultZTRCronExpr))
	}

	if !isValidCronExpression(config.AppConfig.HeartbeatInterval) {
		config.AppConfig.HeartbeatInterval = DefaultHeartbeatCronExpr
		warnings = append(warnings, fmt.Sprintf("invalid schedule provided for heartbeat_interval, using default schedule %s", DefaultHeartbeatCronExpr))
	}

	tlsWarnings, tlsErr := validateTLSConfig(&config.AppConfig.TLS)
	warnings = append(warnings, tlsWarnings...)
	if tlsErr != nil {
		return nil, warnings, tlsErr
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

// validateTLSConfig validates TLS configuration.
func validateTLSConfig(tls *TLSConfig) ([]string, error) {
	var warnings []string

	if tls.CertFile == "" && tls.KeyFile == "" && tls.CAFile == "" {
		return warnings, nil
	}

	if (tls.CertFile != "" && tls.KeyFile == "") || (tls.CertFile == "" && tls.KeyFile != "") {
		return warnings, fmt.Errorf("both cert_file and key_file must be provided together")
	}

	if tls.CertFile != "" {
		if _, err := os.Stat(tls.CertFile); os.IsNotExist(err) {
			return warnings, fmt.Errorf("TLS cert_file not found: %s", tls.CertFile)
		}
	}

	if tls.KeyFile != "" {
		if _, err := os.Stat(tls.KeyFile); os.IsNotExist(err) {
			return warnings, fmt.Errorf("TLS key_file not found: %s", tls.KeyFile)
		}
	}

	if tls.CAFile != "" {
		if _, err := os.Stat(tls.CAFile); os.IsNotExist(err) {
			return warnings, fmt.Errorf("TLS ca_file not found: %s", tls.CAFile)
		}
	}

	if tls.SkipVerify {
		warnings = append(warnings, "TLS skip_verify is enabled, certificate verification will be skipped")
	}

	return warnings, nil
}
