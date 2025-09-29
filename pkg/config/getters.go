package config

import "encoding/json"

// Threadsafe getter functions to fetch config data.

func (cm *ConfigManager) IsZTRDone() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.StateConfig.RegistryCredentials.Username != ""
}

func (cm *ConfigManager) GetLogLevel() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.AppConfig.LogLevel
}

func (cm *ConfigManager) IsJSONLog() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.JsonLog
}

func (cm *ConfigManager) GetOwnRegistry() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.AppConfig.BringOwnRegistry
}

func (cm *ConfigManager) GetZotURL() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return string(cm.config.AppConfig.LocalRegistryCredentials.URL)
}

func (cm *ConfigManager) UseUnsecure() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.AppConfig.UseUnsecure
}

func (cm *ConfigManager) GetSourceRegistryPassword() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.StateConfig.RegistryCredentials.Password
}

func (cm *ConfigManager) GetSourceRegistryUsername() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.StateConfig.RegistryCredentials.Username
}

func (cm *ConfigManager) GetSourceRegistryURL() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return string(cm.config.StateConfig.RegistryCredentials.URL)
}

func (cm *ConfigManager) GetSourceRegistryCredentials() RegistryCredentials {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.StateConfig.RegistryCredentials
}

func (cm *ConfigManager) GetStateURL() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.StateConfig.StateURL
}

func (cm *ConfigManager) GetRemoteRegistryUsername() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.AppConfig.LocalRegistryCredentials.Username
}

func (cm *ConfigManager) GetRemoteRegistryPassword() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.AppConfig.LocalRegistryCredentials.Password
}

func (cm *ConfigManager) GetRemoteRegistryURL() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return string(cm.config.AppConfig.LocalRegistryCredentials.URL)
}

func (cm *ConfigManager) GetRemoteRegistryCredentials() RegistryCredentials {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.AppConfig.LocalRegistryCredentials
}

func (cm *ConfigManager) GetRegistrationInterval() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.AppConfig.RegisterSatelliteInterval
}

func (cm *ConfigManager) GetStateReplicationInterval() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.AppConfig.StateReplicationInterval
}

func (cm *ConfigManager) GetStateConfig() StateConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.StateConfig
}

// You MUST use ResolveGroundControlURL to get the ground control URL.
func (cm *ConfigManager) ResolveGroundControlURL() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if string(cm.config.AppConfig.GroundControlURL) != "" {
		return string(cm.config.AppConfig.GroundControlURL)
	}

	return cm.DefaultGroundControlURL
}

func (cm *ConfigManager) GetToken() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.Token
}

func (cm *ConfigManager) GetRawZotConfig() json.RawMessage {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.ZotConfigRaw
}

func (cm *ConfigManager) GetConfig() *Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config
}

func (cm *ConfigManager) GetStateReportingInterval() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.HeartbeatConfig.StateReportInterval
}

func (cm *ConfigManager) IsHeartbeatDisabled() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.HeartbeatConfig.Disabled
}
