package config

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	_ "github.com/joho/godotenv/autoload"
	"github.com/spf13/viper"
)

var appConfig *Config

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
	GroundControlURL    string              `json:"ground_control_url"`
	LogLevel            string              `json:"log_level"`
	UseUnsecure         bool                `json:"use_unsecure"`
	ZotConfigPath       string              `json:"zot_config_path"`
	Token               string              `json:"token"`
	Jobs                []Job               `json:"jobs"`
	LocalRegistryConfig LocalRegistryConfig `json:"local_registry"`
}

type StateConfig struct {
	Auth   Auth     `json:"auth,omitempty"`
	States []string `json:"states,omitempty"`
}

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

type Job struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
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
	var validJobs []Job
	for i := range config.LocalJsonConfig.Jobs {
		if warning, err := ValidateCronJob(&config.LocalJsonConfig.Jobs[i]); err != nil {
			checks = append(checks, err)
		} else {
			if warning != "" {
				warnings = append(warnings, warning)
			}
			validJobs = append(validJobs, config.LocalJsonConfig.Jobs[i])
		}
	}
	// Add essential jobs if they are not present
	AddEssentialJobs(&validJobs)
	config.LocalJsonConfig.Jobs = validJobs
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
