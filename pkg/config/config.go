package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/robfig/cron/v3"
)

const DefaultSchedule = "@every 00h00m10s"

type RegistryCredentials struct {
	URL      URL    `json:"url,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type AppConfig struct {
	GroundControlURL          URL                 `json:"ground_control_url"`
	LogLevel                  LogLevel            `json:"log_level,omitempty"`
	UseUnsecure               bool                `json:"use_unsecure,omitempty"`
	ZotConfigPath             string              `json:"zot_config_path,omitempty"`
	StateReplicationInterval  string              `json:"state_replication_interval,omitempty"`
	UpdateConfigInterval      string              `json:"update_config_interval,omitempty"`
	RegisterSatelliteInterval string              `json:"register_satellite_interval,omitempty"`
	BringOwnRegistry          bool                `json:"bring_own_registry,omitempty"`
	LocalRegistryCredentials  RegistryCredentials `json:"local_registry"`
}

// TODO: Might need to update ground control code for this to work.
type StateConfig struct {
	RegistryCredentials RegistryCredentials `json:"auth,omitempty"`
	StateURL            string              `json:"state,omitempty"`
}

type Config struct {
	StateConfig StateConfig `json:"state_config,omitempty"`
	AppConfig   AppConfig   `json:"app_config,omitempty"`
}

type URL string

func (v *URL) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if _, err := url.ParseRequestURI(raw); err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	*v = URL(raw)
	return nil
}

type LogLevel string

var validLogLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

func (l *LogLevel) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw != "" && !validLogLevels[strings.ToLower(raw)] {
		return fmt.Errorf("invalid log_level: %s", raw)
	}
	*l = LogLevel(raw)
	return nil
}

type ConfigManager struct {
	config                  *Config
	Token                   string
	DefaultGroundControlURL string
	configPath              string
	mu                      sync.RWMutex
}

func NewConfigManager(path, token, defaultGroundControlURL string, config *Config) (*ConfigManager, error) {
	return &ConfigManager{
		config:                  config,
		configPath:              path,
		Token:                   token,
		DefaultGroundControlURL: defaultGroundControlURL,
	}, nil
}

// Reads the config at the given path and loads it in the given config variable
func ReadAndReturnConfig(path string) (*Config, error) {
	data, err := os.ReadFile(DefaultConfigPath)
	if err != nil {
		return nil, err
	}

	var cfg *Config
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func ValidateConfig(config *Config) []string {
	var warnings []string

	if !isValidCronExpression(config.AppConfig.StateReplicationInterval) {
		config.AppConfig.StateReplicationInterval = DefaultSchedule
		warnings = append(warnings, fmt.Sprintf("invalid schedule provided for StateReplicationInterval, using default schedule %s", DefaultSchedule))
	}

	if !isValidCronExpression(config.AppConfig.RegisterSatelliteInterval) {
		config.AppConfig.RegisterSatelliteInterval = DefaultSchedule
		warnings = append(warnings, fmt.Sprintf("invalid schedule provided for StateReplicationInterval, using default schedule %s", DefaultSchedule))
	}

	if !isValidCronExpression(config.AppConfig.UpdateConfigInterval) {
		config.AppConfig.UpdateConfigInterval = DefaultSchedule
		warnings = append(warnings, fmt.Sprintf("invalid schedule provided for StateReplicationInterval, using default schedule %s", DefaultSchedule))
	}

	return warnings
}

func (cm *ConfigManager) WriteConfig() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := json.MarshalIndent(cm.config, "", "  ")
	if err != nil {
		return err
	}

	err = os.WriteFile(cm.configPath, data, 0644)
	if err != nil {
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

func (cm *ConfigManager) With(mutators ...func(*Config)) *ConfigManager {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for _, mutate := range mutators {
		mutate(cm.config)
	}
	return cm
}

func (cm *ConfigManager) IsZTRDone() bool {
	return cm.GetSourceRegistryURL() != ""
}
