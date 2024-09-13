package config

import (
	"fmt"

	"github.com/spf13/viper"
)

var AppConfig *Config

type Config struct {
	logLevel        string
	ownRegistry     bool
	ownRegistryAdr  string
	ownRegistryPort string
	zotConfigPath   string
	input           string
	zot_url         string
	registry        string
	repository      string
	user_input      string
	scheme          string
	api_version     string
	image           string
}

func GetLogLevel() string {
	return AppConfig.logLevel
}

func GetOwnRegistry() bool {
	return AppConfig.ownRegistry
}

func GetOwnRegistryAdr() string {
	return AppConfig.ownRegistryAdr
}

func GetOwnRegistryPort() string {
	return AppConfig.ownRegistryPort
}

func GetZotConfigPath() string {
	return AppConfig.zotConfigPath
}

func GetInput() string {
	return AppConfig.input
}

func SetZotURL(url string) {
	AppConfig.zot_url = url
}

func GetZotURL() string {
	return AppConfig.zot_url
}

func SetRegistry(registry string) {
	AppConfig.registry = registry
}

func GetRegistry() string {
	return AppConfig.registry
}

func SetRepository(repository string) {
	AppConfig.repository = repository
}

func GetRepository() string {
	return AppConfig.repository
}

func SetUserInput(user_input string) {
	AppConfig.user_input = user_input
}

func GetUserInput() string {
	return AppConfig.user_input
}

func SetScheme(scheme string) {
	AppConfig.scheme = scheme
}

func GetScheme() string {
	return AppConfig.scheme
}

func SetAPIVersion(api_version string) {
	AppConfig.api_version = api_version
}

func GetAPIVersion() string {
	return AppConfig.api_version
}

func SetImage(image string) {
	AppConfig.image = image
}

func GetImage() string {
	return AppConfig.image
}

func LoadConfig() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	return &Config{
		logLevel:        viper.GetString("log_level"),
		ownRegistry:     viper.GetBool("bring_own_registry"),
		ownRegistryAdr:  viper.GetString("own_registry_adr"),
		ownRegistryPort: viper.GetString("own_registry_port"),
		zotConfigPath:   viper.GetString("zotConfigPath"),
		input:           viper.GetString("url_or_file"),
	}, nil
}

func InitConfig() error {
	var err error
	AppConfig, err = LoadConfig()
	if err != nil {
		return err
	}
	return nil
}
