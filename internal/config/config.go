package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

var AppConfig *Config

type Config struct {
	log_level         string
	own_registry      bool
	own_registry_adr  string
	own_registry_port string
	zot_config_path   string
	input             string
	zot_url           string
	registry          string
	repository        string
	user_input        string
	scheme            string
	api_version       string
	image             string
	harbor_password   string
	harbor_username   string
	env               string
	use_unsecure      bool
}

func GetLogLevel() string {
	return AppConfig.log_level
}

func GetOwnRegistry() bool {
	return AppConfig.own_registry
}

func GetOwnRegistryAdr() string {
	return AppConfig.own_registry_adr
}

func GetOwnRegistryPort() string {
	return AppConfig.own_registry_port
}

func GetZotConfigPath() string {
	return AppConfig.zot_config_path
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

func UseUnsecure() bool {
	return AppConfig.use_unsecure
}

func GetHarborPassword() string {
	return AppConfig.harbor_password
}

func GetHarborUsername() string {
	return AppConfig.harbor_username
}

func LoadConfig() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("error reading config file at path '%s': %w", viper.ConfigFileUsed(), err)
	}

	// Load environment and start satellite
	if err := godotenv.Load(); err != nil {
		return &Config{}, fmt.Errorf("error loading .env file: %w", err)
	}
	var use_unsecure bool
	if os.Getenv("USE_UNSECURE") == "true" {
		use_unsecure = true
	} else {
		use_unsecure = false
	}

	return &Config{
		log_level:         viper.GetString("log_level"),
		own_registry:      viper.GetBool("bring_own_registry"),
		own_registry_adr:  viper.GetString("own_registry_adr"),
		own_registry_port: viper.GetString("own_registry_port"),
		zot_config_path:   viper.GetString("zotConfigPath"),
		input:             viper.GetString("url_or_file"),
		harbor_password:   os.Getenv("HARBOR_PASSWORD"),
		harbor_username:   os.Getenv("HARBOR_USERNAME"),
		env:               os.Getenv("ENV"),
		zot_url:           os.Getenv("ZOT_URL"),
		use_unsecure:      use_unsecure,
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
