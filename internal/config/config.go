package config

import (
	"encoding/json"
	"fmt"
	"github.com/robfig/cron/v3"
	"os"
)

var appConfig *Config

const DefaultSchedule = "@every 00h00m10s"

// Warning represents a non-critical issue with configuration.
type Warning string

type Auth struct {
	SourceUsername string `json:"name,omitempty"`
	Registry       string `json:"registry,omitempty"`
	SourcePassword string `json:"secret,omitempty"`
}

type LocalRegistryConfig struct {
	URL              string `json:"url"`
	UserName         string `json:"username"`
	Password         string `json:"password"`
	BringOwnRegistry bool   `json:"bring_own_registry"`
}

// LocalJsonConfig is a struct that holds the configs that are passed as environment variables
type LocalJsonConfig struct {
	GroundControlURL          string              `json:"ground_control_url"`
	LogLevel                  string              `json:"log_level"`
	UseUnsecure               bool                `json:"use_unsecure"`
	ZotConfigPath             string              `json:"zot_config_path"`
	Token                     string              `json:"token"`
	StateReplicationInterval  string              `json:"state_replication_interval"`
	UpdateConfigInterval      string              `json:"update_config_interval"`
	RegisterSatelliteInterval string              `json:"register_satellite_interval"`
	LocalRegistryConfig       LocalRegistryConfig `json:"local_registry"`
}

type StateConfig struct {
	Auth   Auth     `json:"auth,omitempty"`
	States []string `json:"states,omitempty"`
}

type Config struct {
	StateConfig     StateConfig     `json:"state_config,omitempty"`
	LocalJsonConfig LocalJsonConfig `json:"environment_variables,omitempty"`
	ZotUrl          string          `json:"zot_url,omitempty"`
}

type Job struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
}

// ParseConfigFromJson parses a JSON string into a Config struct. Returns an error if the JSON is invalid
func ParseConfigFromJson(jsonData string) (*Config, error) {
	var config Config
	err := json.Unmarshal([]byte(jsonData), &config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}

// ReadConfigData reads the data from the specified path. Returns an error if the file does not exist or is a directory
func ReadConfigData(configPath string) ([]byte, error) {
	fileInfo, err := os.Stat(configPath)
	if err != nil {
		return nil, err
	}
	if fileInfo.IsDir() {
		return nil, os.ErrNotExist
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// LoadConfig reads the configuration file from the specified path and returns a Config struct. Returns an error if the file does not exist or is a directory.
// Also returns a slice of errors and warnings if the configuration is invalid
// For jobs, we will do the following:
// 1. Check the jobs provided by the user in the config.json.
// 2. Validate the jobs provided by the user.
// 3. If the job cron schedule is not valid, set the default schedule and replace it in the jobs.
// 4. Once the job is validated, append it to the validJobs slice if the job name is valid, i.e., it is used by the satellite.
// 5. Finally, check for critical jobs that are not present in the config and manually add them to the validJobs slice.
func LoadConfig(configPath string) (*Config, []error, []Warning) {
	var checks []error
	var warnings []Warning
	var err error
	configData, err := ReadConfigData(configPath)
	if err != nil {
		checks = append(checks, err)
		return nil, checks, warnings
	}
	config, err := ParseConfigFromJson(string(configData))
	if err != nil {
		checks = append(checks, err)
		return nil, checks, warnings
	}

	// Validate the job schedule fields
	if config.LocalJsonConfig.StateReplicationInterval != "" {
		if !isValidCronExpression(config.LocalJsonConfig.StateReplicationInterval) {
			cronWarning := Warning(fmt.Sprintf("no schedule provided for StateReplicationInterval, using default schedule %s", DefaultSchedule))
			warnings = append(warnings, cronWarning)
			config.LocalJsonConfig.StateReplicationInterval = DefaultSchedule
		}
	}
	if config.LocalJsonConfig.RegisterSatelliteInterval != "" {
		if !isValidCronExpression(config.LocalJsonConfig.RegisterSatelliteInterval) {
			cronWarning := Warning(fmt.Sprintf("no schedule provided for RegisterSatelliteInterval, using default schedule %s", DefaultSchedule))
			warnings = append(warnings, cronWarning)
			config.LocalJsonConfig.RegisterSatelliteInterval = DefaultSchedule
		}
	}
	if config.LocalJsonConfig.UpdateConfigInterval != "" {
		if !isValidCronExpression(config.LocalJsonConfig.UpdateConfigInterval) {
			cronWarning := Warning(fmt.Sprintf("no schedule provided for UpdateConfigInterval, using default schedule %s", DefaultSchedule))
			warnings = append(warnings, cronWarning)
			config.LocalJsonConfig.UpdateConfigInterval = DefaultSchedule
		}
	}

	return config, checks, warnings
}

// InitConfig reads the configuration file from the specified path and initializes the global appConfig variable.
func InitConfig(configPath string) ([]error, []Warning) {
	var err []error
	var warnings []Warning
	appConfig, err, warnings = LoadConfig(configPath)
	WriteConfig(configPath)
	return err, warnings
}

func UpdateStateAuthConfig(name, registry, secret string, states []string) {
	appConfig.StateConfig.Auth.SourceUsername = name
	appConfig.StateConfig.Auth.Registry = registry
	appConfig.StateConfig.Auth.SourcePassword = secret
	appConfig.StateConfig.States = states
	WriteConfig(DefaultConfigPath)
}

func WriteConfig(configPath string) error {
	data, err := json.MarshalIndent(appConfig, "", "  ")
	if err != nil {
		return err
	}
	err = os.WriteFile(configPath, data, 0644)
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
