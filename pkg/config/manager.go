package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
)

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

func (cm *ConfigManager) With(mutators ...func(*Config)) *ConfigManager {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for _, mutate := range mutators {
		mutate(cm.config)
	}
	return cm
}

// Writes the cm's config to disk
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

// Writes the given config to disk
func (cm *ConfigManager) WriteConfigToDisk(config *Config) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	err = os.WriteFile(cm.configPath, data, 0644)
	if err != nil {
		return err
	}

	return nil
}

func InitConfigManager(path string) (*ConfigManager, []string, error) {
	var cfg *Config
	var err error

	// Mandatory ENV vars
	token, ok := os.LookupEnv("TOKEN")
	if !ok {
		return nil, nil, fmt.Errorf("satellite token not present as environment variable")
	}

	gcURL, ok := os.LookupEnv("GROUND_CONTROL_URL")
	if !ok {
		return nil, nil, fmt.Errorf("satellite ground control URL not present as environment variable")
	}

	cfg, err = readAndReturnConfig(path)
	if errors.Is(err, os.ErrNotExist) {
		// config.json doesn't exist, create with sane defaults
		cfg = &Config{
			AppConfig: AppConfig{
				GroundControlURL: URL(gcURL),
			},
			ZotConfigRaw: json.RawMessage(DefaultZotConfigJSON),
		}
	} else if err != nil {
		// config.json exists but is malformed
		return nil, nil, fmt.Errorf("failed to read config: %w", err)
	}

	// Validate config and collect non-fatal warnings
	warnings, err := ValidateConfig(cfg)
	if err != nil {
		return nil, warnings, fmt.Errorf("invalid config: %w", err)
	}

	cm, err := NewConfigManager(path, token, gcURL, cfg)
	if err != nil {
		return nil, warnings, fmt.Errorf("failed to create config manager: %w", err)
	}

	return cm, warnings, nil
}

// Reads the config at the given path and returns the parsed Config
func readAndReturnConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
