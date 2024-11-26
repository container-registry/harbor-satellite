package config

import (
	"encoding/json"
	"fmt"
	"os"
)

var appConfig *Config

const DefaultConfigPath string = "config.json"

type Auth struct {
	Name     string `json:"name"`
	Registry string `json:"registry"`
	Secret   string `json:"secret"`
}

type Config struct {
	Auth               Auth     `json:"auth"`
	BringOwnRegistry   bool     `json:"bring_own_registry"`
	GroundControlURL   string   `json:"ground_control_url"`
	LogLevel           string   `json:"log_level"`
	OwnRegistryAddress string   `json:"own_registry_adr"`
	OwnRegistryPort    string   `json:"own_registry_port"`
	States             []string `json:"states"`
	URLOrFile          string   `json:"url_or_file"`
	ZotConfigPath      string   `json:"zotconfigpath"`
	UseUnsecure        bool     `json:"use_unsecure"`
	ZotUrl             string   `json:"zot_url"`
	StateFetchPeriod   string   `json:"state_fetch_period"`
}

func GetLogLevel() string {
	return appConfig.LogLevel
}

func GetOwnRegistry() bool {
	return appConfig.BringOwnRegistry
}

func GetOwnRegistryAdr() string {
	return appConfig.OwnRegistryAddress
}

func GetOwnRegistryPort() string {
	return appConfig.OwnRegistryPort
}

func GetZotConfigPath() string {
	return appConfig.ZotConfigPath
}

func GetInput() string {
	return appConfig.URLOrFile
}

func SetZotURL(url string) {
	appConfig.ZotUrl = url
}

func GetZotURL() string {
	return appConfig.ZotUrl
}

func UseUnsecure() bool {
	return appConfig.UseUnsecure
}

func GetHarborPassword() string {
	return appConfig.Auth.Secret
}

func GetHarborUsername() string {
	return appConfig.Auth.Name
}

func SetRemoteRegistryURL(url string) {
	appConfig.Auth.Registry = url
}

func GetRemoteRegistryURL() string {
	return appConfig.Auth.Registry
}

func GetStateFetchPeriod() string {
	return appConfig.StateFetchPeriod
}

func GetStates() []string {
	return appConfig.States
}

func ParseConfigFromJson(jsonData string) (*Config, error) {
	var config Config
	err := json.Unmarshal([]byte(jsonData), &config)
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
