package config

func GetLogLevel() string {
	if appConfig == nil || appConfig.LocalJsonConfig.LogLevel == "" {
		return "info"
	}
	return appConfig.LocalJsonConfig.LogLevel
}

func GetOwnRegistry() bool {
	return appConfig.LocalJsonConfig.LocalRegistryConfig.BringOwnRegistry
}

func GetZotConfigPath() string {
	return appConfig.LocalJsonConfig.ZotConfigPath
}

func GetZotURL() string {
	return appConfig.ZotUrl
}

func UseUnsecure() bool {
	return appConfig.LocalJsonConfig.UseUnsecure
}

func GetSourceRegistryPassword() string {
	return appConfig.StateConfig.Auth.SourcePassword
}

func GetSourceRegistryUsername() string {
	return appConfig.StateConfig.Auth.SourceUsername
}

func GetSourceRegistryURL() string {
	return appConfig.StateConfig.Auth.Registry
}

func GetState() string {
	return appConfig.StateConfig.State
}

func GetToken() string {
	return appConfig.LocalJsonConfig.Token
}

func GetGroundControlURL() string {
	return appConfig.LocalJsonConfig.GroundControlURL
}

func GetRemoteRegistryUsername() string {
	return appConfig.LocalJsonConfig.LocalRegistryConfig.UserName
}

func GetRemoteRegistryPassword() string {
	return appConfig.LocalJsonConfig.LocalRegistryConfig.Password
}

func GetRemoteRegistryURL() string {
	return appConfig.LocalJsonConfig.LocalRegistryConfig.URL
}

func GetRegistrationInterval() string {
	return appConfig.LocalJsonConfig.RegisterSatelliteInterval
}

func GetUpdateConfigInterval() string {
	return appConfig.LocalJsonConfig.UpdateConfigInterval
}

func GetStateReplicationInterval() string {
	return appConfig.LocalJsonConfig.StateReplicationInterval
}
