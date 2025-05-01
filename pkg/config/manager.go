package config

import (
	"encoding/json"
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

func InitConfigManager(path string) (*ConfigManager, []string, error) {
	cfg, err := readAndReturnConfig(path)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read config: %w", err)
	}

	warnings, err := ValidateConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid config: %w", err)
	}

	token := os.Getenv("TOKEN")
	defaultGroundControlURL := os.Getenv("GROUND_CONTROL_URL")

	cm, err := NewConfigManager(path, token, defaultGroundControlURL, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create manager: %w", err)
	}

	return cm, warnings, nil
}

// Reads the config at the given path and loads it in the given config variable
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
