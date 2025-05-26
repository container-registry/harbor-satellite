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
	JsonLog                 bool
	configPath              string
	prevConfigPath          string
	mu                      sync.RWMutex
}

func NewConfigManager(configPath, prevConfigPath, token, defaultGroundControlURL string, jsonLog bool, config *Config) (*ConfigManager, error) {
	return &ConfigManager{
		config:                  config,
		configPath:              configPath,
		prevConfigPath:          prevConfigPath,
		Token:                   token,
		DefaultGroundControlURL: defaultGroundControlURL,
		JsonLog:                 jsonLog,
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

func InitConfigManager(path string, jsonLog bool) (*ConfigManager, []string, error) {
	cfg, err := readAndReturnConfig(path)
	if err != nil {
		return err
	}

	return nil
}

func InitConfigManager(token, groundControlURL, configPath, prevConfigPath string) (*ConfigManager, []string, error) {
	var cfg *Config
	var err error

	if _, err := url.ParseRequestURI(groundControlURL); err != nil {
		return nil, nil, fmt.Errorf("invalid URL provided for ground_control_url env var: %w", err)
	}

	cfg, err = readAndReturnConfig(configPath)
	if errors.Is(err, os.ErrNotExist) {
		cfg = &Config{}
	} else if err != nil {
		return nil, nil, fmt.Errorf("failed to read config: %w", err)
	}

	cfg, warnings, err := ValidateAndEnforceDefaults(cfg, groundControlURL)
	if err != nil {
		return nil, warnings, fmt.Errorf("invalid config: %w", err)
	}

	cm, err := NewConfigManager(configPath, prevConfigPath, token, groundControlURL, cfg)
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
