package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/robfig/cron/v3"
)

const DefaultSchedule = "@every 00h00m10s"

// Warning represents a non-critical issue with configuration.
type Warning string

type RegistryCredentials struct {
	URL      string `json:"url,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type AppConfig struct {
	GroundControlURL          string              `json:"ground_control_url"`
	LogLevel                  string              `json:"log_level,omitempty"`
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

type ConfigManager struct {
	config     *Config
	configPath string
	mu         sync.RWMutex
}

func NewConfigManager(path string) (*ConfigManager, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg *Config
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return &ConfigManager{
		config:     cfg,
		configPath: path,
	}, nil
}

func ValidateConfig(config Config) []string {
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

func (cm *ConfigManager) SetStateAuthConfig(username, registryURL, password, stateURL string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.config.StateConfig.RegistryCredentials.Username = username
	cm.config.StateConfig.RegistryCredentials.URL = registryURL
	cm.config.StateConfig.RegistryCredentials.Password = password
	cm.config.StateConfig.StateURL = stateURL
}

func (cm *ConfigManager) SetGroundControlURL(groundControlURL string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.config.AppConfig.GroundControlURL = groundControlURL
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
