package config

func (cm *ConfigManager) GetLogLevel() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return string(cm.config.AppConfig.LogLevel)
}

func (cm *ConfigManager) GetOwnRegistry() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.AppConfig.BringOwnRegistry
}

func (cm *ConfigManager) GetZotConfigPath() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.AppConfig.ZotConfigPath
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

func (cm *ConfigManager) GetGroundControlURL() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return string(cm.config.AppConfig.GroundControlURL)
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

func (cm *ConfigManager) GetUpdateConfigInterval() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.AppConfig.UpdateConfigInterval
}

func (cm *ConfigManager) GetStateReplicationInterval() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.AppConfig.StateReplicationInterval
}

// You MUST use ResolveGroundControlURL to get the ground control URL.
func (cm *ConfigManager) ResolveGroundControlURL() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.GetGroundControlURL() != "" {
		return (cm.GetGroundControlURL())
	}

	return (cm.DefaultGroundControlURL)
}
