package config

import (
	"encoding/json"
	"fmt"
	"os"
)

var appConfig *Config

const DefaultConfigPath string = "config.json"

type Auth struct {
	Name     string `json:"name,omitempty"`
	Registry string `json:"registry,omitempty"`
	Secret   string `json:"secret,omitempty"`
}

// LocalJsonConfig is a struct that holds the configs that are passed as environment variables
type LocalJsonConfig struct {
	BringOwnRegistry  bool   `json:"bring_own_registry"`
	GroundControlURL  string `json:"ground_control_url"`
	LogLevel          string `json:"log_level"`
	OwnRegistryAddr   string `json:"own_registry_addr"`
	OwnRegistryPort   string `json:"own_registry_port"`
	UseUnsecure       bool   `json:"use_unsecure"`
	ZotConfigPath     string `json:"zot_config_path"`
	Token             string `json:"token"`
	StateFetchPeriod  string `json:"state_fetch_period"`
	ConfigFetchPeriod string `json:"config_fetch_period"`
}

type StateConfig struct {
	Auth   Auth     `json:"auth,omitempty"`
	States []string `json:"states,omitempty"`
}

type Config struct {
	StateConfig     StateConfig     `json:"state_config"`
	LocalJsonConfig LocalJsonConfig `json:"environment_variables"`
	ZotUrl          string          `json:"zot_url"`
}

func GetLogLevel() string {
	return appConfig.LocalJsonConfig.LogLevel
}

func GetOwnRegistry() bool {
	return appConfig.LocalJsonConfig.BringOwnRegistry
}

func GetOwnRegistryAdr() string {
	return appConfig.LocalJsonConfig.OwnRegistryAddr
}

func GetOwnRegistryPort() string {
	return appConfig.LocalJsonConfig.OwnRegistryPort
}

func GetZotConfigPath() string {
	return appConfig.LocalJsonConfig.ZotConfigPath
}

func GetInput() string {
	return ""
}

func SetZotURL(url string) {
	appConfig.ZotUrl = url
}

func GetZotURL() string {
	return appConfig.ZotUrl
}

func UseUnsecure() bool {
	return appConfig.LocalJsonConfig.UseUnsecure
}

func GetHarborPassword() string {
	return appConfig.StateConfig.Auth.Secret
}

func GetHarborUsername() string {
	return appConfig.StateConfig.Auth.Name
}

func SetRemoteRegistryURL(url string) {
	appConfig.StateConfig.Auth.Registry = url
}

func GetRemoteRegistryURL() string {
	return appConfig.StateConfig.Auth.Registry
}

func GetStateFetchPeriod() string {
	return appConfig.LocalJsonConfig.StateFetchPeriod
}

func GetConfigFetchPeriod() string {
	return appConfig.LocalJsonConfig.ConfigFetchPeriod
}

func GetStates() []string {
	return appConfig.StateConfig.States
}

func GetToken() string {
	return appConfig.LocalJsonConfig.Token
}

func GetGroundControlURL() string {
	return appConfig.LocalJsonConfig.GroundControlURL
}

func SetGroundControlURL(url string) {
	appConfig.LocalJsonConfig.GroundControlURL = url
}

func ParseConfigFromJson(jsonData string) (*Config, error) {
	var config Config
	err := json.Unmarshal([]byte(jsonData), &config.LocalJsonConfig)
	if err != nil {
		return nil, err
	}
	return &config, nil
}

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

func LoadConfig() (*Config, error) {
	configData, err := ReadConfigData(DefaultConfigPath)
	if err != nil {
		fmt.Printf("Error reading config file: %v\n", err)
		return nil, err
	}
	config, err := ParseConfigFromJson(string(configData))
	if err != nil {
		fmt.Printf("Error parsing config file: %v\n", err)
		return nil, err
	}
	return config, nil
}

func InitConfig() error {
	var err error
	appConfig, err = LoadConfig()
	if err != nil {
		return err
	}
	return nil
}

func UpdateStateConfig(name, registry, secret string, states []string) {
	appConfig.StateConfig.Auth.Name = name
	appConfig.StateConfig.Auth.Registry = registry
	appConfig.StateConfig.Auth.Secret = secret
	appConfig.StateConfig.States = states
}
