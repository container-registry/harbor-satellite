package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type ConfigChangeType string

const (
	LogLevelChanged  ConfigChangeType = "log_level"
	IntervalsChanged ConfigChangeType = "intervals"
	ZotConfigChanged ConfigChangeType = "zot_config"
)

type ConfigChange struct {
	Type     ConfigChangeType
	OldValue interface{}
	NewValue interface{}
}

type ConfigChangeCallback func(change ConfigChange) error

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

// Writes the given config to disk at the configPath
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

// Writes the given config to disk at the prevConfigPath
func (cm *ConfigManager) WritePrevConfigToDisk(config *Config) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	err = os.WriteFile(cm.prevConfigPath, data, 0644)
	if err != nil {
		return err
	}

	return nil
}

func (cm *ConfigManager) detectChanges(oldConfig *Config, newConfig *Config) []ConfigChange {
	var changes []ConfigChange

	if oldConfig.AppConfig.LogLevel != newConfig.AppConfig.LogLevel {
		changes = append(changes, ConfigChange{
			Type:     LogLevelChanged,
			OldValue: oldConfig.AppConfig.LogLevel,
			NewValue: newConfig.AppConfig.LogLevel,
		})
	}

	if oldConfig.AppConfig.StateReplicationInterval != newConfig.AppConfig.StateReplicationInterval {
		changes = append(changes, ConfigChange{
			Type:     IntervalsChanged,
			OldValue: oldConfig.AppConfig.StateReplicationInterval,
			NewValue: newConfig.AppConfig.StateReplicationInterval,
		})
	}

	if oldConfig.AppConfig.HeartbeatInterval != newConfig.AppConfig.HeartbeatInterval {
		changes = append(changes, ConfigChange{
			Type:     IntervalsChanged,
			OldValue: oldConfig.AppConfig.HeartbeatInterval,
			NewValue: newConfig.AppConfig.HeartbeatInterval,
		})
	}

	if string(oldConfig.ZotConfigRaw) != string(newConfig.ZotConfigRaw) {
		changes = append(changes, ConfigChange{
			Type:     ZotConfigChanged,
			OldValue: "zot_config_changed",
			NewValue: "zot_config_changed",
		})
	}

	return changes
}
func (cm *ConfigManager) ReloadConfig() ([]ConfigChange, []string, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	oldConfig := cm.config

	newConfig, err := readAndReturnConfig(cm.configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read config from disk: %w", err)
	}

	validatedConfig, warnings, err := ValidateAndEnforceDefaults(newConfig, cm.DefaultGroundControlURL)
	if err != nil {
		return nil, warnings, fmt.Errorf("failed to validate reloaded config: %w", err)
	}

	changes := cm.detectChanges(oldConfig, validatedConfig)

	cm.config = validatedConfig

	return changes, warnings, nil

}

func InitConfigManager(token, groundControlURL, configPath, prevConfigPath string, jsonLogging, useUnsecure bool) (*ConfigManager, []string, error) {
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

	// Override use_unsecure from CLI/env if set
	if useUnsecure {
		cfg.AppConfig.UseUnsecure = true
	}

	cfg, warnings, err := ValidateAndEnforceDefaults(cfg, groundControlURL)
	if err != nil {
		return nil, warnings, fmt.Errorf("invalid config: %w", err)
	}

	cm, err := NewConfigManager(configPath, prevConfigPath, token, groundControlURL, jsonLogging, cfg)
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

type RefreshCredentialsResponse struct {
	RobotName string `json:"robot_name"`
	Secret    string `json:"secret"`
}

func (cm *ConfigManager) RefreshCredentials(ctx context.Context) error {
	// 1. Acquire lock to read configuration
	cm.mu.RLock()
	satelliteName := cm.config.StateConfig.SatelliteName
	
	baseURL := cm.DefaultGroundControlURL
	if baseURL == "" {
		baseURL = os.Getenv("GROUND_CONTROL_URL")
	}
	cm.mu.RUnlock()

	// Validate configuration
	if satelliteName == "" {
		return fmt.Errorf("satellite name not set in config")
	}
	if baseURL == "" {
		return fmt.Errorf("ground control URL not set")
	}

	creds, err := cm.performCredentialRefreshRequest(ctx, baseURL, satelliteName)
	if err != nil {
		return err
	}

	// Validate response before persisting
	if creds.RobotName == "" || creds.Secret == "" {
		return fmt.Errorf("invalid refresh response: received empty credentials")
	}

	// 3. Acquire lock to update configuration
	cm.mu.Lock()
	cm.config.StateConfig.RegistryCredentials.Username = creds.RobotName
	cm.config.StateConfig.RegistryCredentials.Password = creds.Secret
	cm.mu.Unlock() // Unlock before calling WriteConfig to avoid deadlock
	
	// Persist
	return cm.WriteConfig()
}

func (cm *ConfigManager) performCredentialRefreshRequest(ctx context.Context, baseURL, satelliteName string) (*RefreshCredentialsResponse, error) {
	// 2. Perform Network I/O (Unlocked)
	encodedSatelliteName := url.PathEscape(satelliteName)
	refreshURL := fmt.Sprintf("%s/satellites/%s/refresh-credentials", baseURL, encodedSatelliteName)

	req, err := http.NewRequestWithContext(ctx, "POST", refreshURL, nil)
	if err != nil {
		return nil, err
	}
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to refresh credentials: %s, body: %s", resp.Status, string(body))
	}

	var creds RefreshCredentialsResponse
	if err := json.NewDecoder(resp.Body).Decode(&creds); err != nil {
		return nil, err
	}

	return &creds, nil
}
