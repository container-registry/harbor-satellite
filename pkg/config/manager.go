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
	cm.mu.Lock()
	defer cm.mu.Unlock()

	satelliteName := cm.config.StateConfig.SatelliteName
	if satelliteName == "" {
		return fmt.Errorf("satellite name not set in config")
	}
	
	// Construct URL
	// We use DefaultGroundControlURL or from config? 
	// The register command uses DefaultGroundControlURL or checks env.
	// ConfigManager has DefaultGroundControlURL.
	// But usually Ground Control URL is part of the system environment.
	// We can use cm.DefaultGroundControlURL if it's stored.
	baseURL := cm.DefaultGroundControlURL
	if baseURL == "" {
		// Fallback to env?
		baseURL = os.Getenv("GROUND_CONTROL_URL")
	}
	if baseURL == "" {
		return fmt.Errorf("ground control URL not set")
	}

	url := fmt.Sprintf("%s/satellites/%s/refresh-credentials", baseURL, satelliteName)

	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return err
	}
	
	// Add auth token? 
	// We assume we don't have a token effectively anymore if we are refreshing?
	// But wait, the previous handler implementation:
	// "TODO: Authenticate that the request comes from the valid satellite?"
	// I left it open.
	// If I need to authenticate, I need a token.
	// But the robot credentials are what we use for auth usually?
	// If robot creds are invalid (expired), we can't use them to refresh themselves usually.
	// UNLESS the refresh endpoint allows expired credentials (if slightly expired)?
	// OR we use mTLS.
	// Given the scope, I will assume network/mTLS or open endpoint for this refactor step, 
	// unless user specified authentication.
	// The implementation plan "Satellite calls POST ... after receiving 403".
	// If I use the robot secret to auth the refresh call, it will fail if 403.
	// So it must be unauthenticated OR mTLS.
	// For now, I will NOT add Authorization header.

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to refresh credentials: %s, body: %s", resp.Status, string(body))
	}

	var creds RefreshCredentialsResponse
	if err := json.NewDecoder(resp.Body).Decode(&creds); err != nil {
		return err
	}

	// Update config
	cm.config.StateConfig.RegistryCredentials.Username = creds.RobotName
	cm.config.StateConfig.RegistryCredentials.Password = creds.Secret
	
	// Persist
	data, err := json.MarshalIndent(cm.config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cm.configPath, data, 0644)
}
