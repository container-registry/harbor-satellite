package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
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

	token, ok := os.LookupEnv("TOKEN")
	if !ok {
		return nil, nil, fmt.Errorf("TOKEN env var not defined as environment variable")
	}

	gcURL, ok := os.LookupEnv("GROUND_CONTROL_URL")
	if !ok {
		return nil, nil, fmt.Errorf("GROUND_CONTROL_URL not defined as environment variable")
	}
	if _, err := url.ParseRequestURI(gcURL); err != nil {
		return nil, nil, fmt.Errorf("invalid URL provided for ground_control_url env var: %w", err)
	}

	cfg, err = readAndReturnConfig(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg = &Config{}
	} else if err != nil {
		return nil, nil, fmt.Errorf("failed to read config: %w", err)
	}

	warnings, err := ValidateAndEnforceDefaults(cfg, gcURL)
	if err != nil {
		return nil, warnings, fmt.Errorf("invalid config: %w", err)
	}

	cm, err := NewConfigManager(path, token, gcURL, cfg)
	if err != nil {
		return nil, warnings, fmt.Errorf("failed to create config manager: %w", err)
	}

	return cm, warnings, nil
}

// Reads the config at the given path and returns the parsed Config.
func readAndReturnConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "{}" {
		return nil, os.ErrNotExist
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
