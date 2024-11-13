package config

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	_ "github.com/joho/godotenv/autoload"
	"github.com/spf13/viper"
)

var AppConfig *Config

type Config struct {
	log_level          string
	own_registry       bool
	own_registry_adr   string
	own_registry_port  string
	zot_config_path    string
	ground_control_url string
	input              string
	zot_url            string
	registry           string
	repository         string
	user_input         string
	scheme             string
	api_version        string
	image              string
	token              string
	states             []string
	harbor_password    string
	harbor_username    string
	env                string
	use_unsecure       bool
}

type ZtrResult struct {
	States []string `json:"states"`
	Auth   Account  `json:"auth"`
}

type Account struct {
	Name     string `json:"name"`
	Secret   string `json:"secret"`
	Registry string `json:"registry"`
}

// fetch data from Ground Control, parse it
func ztr(gcUrl string) (*ZtrResult, error) {
	// Get environment variables
	token := os.Getenv("TOKEN")

	// Construct the request URL
	gcUrl = fmt.Sprintf("%s/satellites/ztr/%s", gcUrl, token)

	// Make the GET request
	resp, err := http.Get(gcUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch data from Ground Control: %v", err)
	}
	defer resp.Body.Close()

	// Check for a successful response
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected response from Ground Control: %v", resp.Status)
	}

	// Decode the JSON response
	var res ZtrResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed to decode JSON response: %v", err)
	}

	viper.Set("auth.name", res.Auth.Name)         // Set auth name in config
	viper.Set("auth.secret", res.Auth.Secret)     // Set auth secret in config
	viper.Set("auth.registry", res.Auth.Registry) // Set auth registry in config
	viper.Set("states", res.States)               // Set states array in config
	viper.WriteConfigAs("config.gen.json")

	return &res, nil
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

func GetGroundControlURL() string {
	return AppConfig.ground_control_url
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

func GetToken() string {
	return AppConfig.token
}

func GetStates() []string {
	return AppConfig.states
}

func GetHarborPassword() string {
	return AppConfig.harbor_password
}

func GetHarborUsername() string {
	return AppConfig.harbor_username
}

func LoadConfig() (*Config, error) {
	// Check if config.gen.json exists
	if _, err := os.Stat("config.gen.json"); err == nil {
		// If config.gen.json exists, set it as the configuration file
		viper.SetConfigFile("config.gen.json")
		fmt.Println("Using config.gen.json for configuration.")
	} else if os.IsNotExist(err) {
		// fall back to config.toml
		viper.SetConfigName("config")
		viper.SetConfigType("toml")
		viper.AddConfigPath(".")

		if err := viper.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("error reading config file at path '%s': %w", viper.ConfigFileUsed(), err)
		}

		gcURL := viper.GetString("ground_control_url")
		// Call ztr function to fetch and set the necessary configuration
		_, err := ztr(gcURL)
		if err != nil {
			return nil, fmt.Errorf("Error in ztr function: %v\n", err)
		}
	} else {
		return nil, fmt.Errorf("Error checking config file: %v\n", err)
	}

	var use_unsecure bool
	if os.Getenv("USE_UNSECURE") == "true" {
		use_unsecure = true
	} else {
		use_unsecure = false
	}

	return &Config{
		log_level:          viper.GetString("log_level"),
		own_registry:       viper.GetBool("bring_own_registry"),
		own_registry_adr:   viper.GetString("own_registry_adr"),
		own_registry_port:  viper.GetString("own_registry_port"),
		zot_config_path:    viper.GetString("zotConfigPath"),
		ground_control_url: viper.GetString("ground_control_url"),
		input:              viper.GetString("url_or_file"),
		token:              os.Getenv("TOKEN"),
		states:             viper.GetStringSlice("states"),
		harbor_password:    viper.GetString("auth.secret"),
		harbor_username:    viper.GetString("auth.name"),
		env:                os.Getenv("ENV"),
		zot_url:            os.Getenv("ZOT_URL"),
		use_unsecure:       use_unsecure,
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
