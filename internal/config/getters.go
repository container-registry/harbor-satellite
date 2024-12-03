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

func SetRemoteRegistryURL(url string) {
	appConfig.LocalJsonConfig.LocalRegistryConfig.URL = url
	WriteConfig(DefaultConfigPath)
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

func SetSourceRegistryURL(url string) {
	appConfig.StateConfig.Auth.Registry = url
}

func GetSourceRegistryURL() string {
	return appConfig.StateConfig.Auth.Registry
}

func GetStates() []string {
	return appConfig.StateConfig.States
}

func GetToken() string {
	return appConfig.LocalJsonConfig.Token
}

func GetGroundControlURL() string {
	return appConfig.LocalJsonConfig.GroundControlURL
}

func SetGroundControlURL(url string) {
	appConfig.LocalJsonConfig.GroundControlURL = url
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
