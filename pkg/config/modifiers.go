package config

func SetStateURL(url string) func(*Config) {
	return func(cfg *Config) {
		cfg.StateConfig.StateURL = url
	}
}

func SetStateAuth(username, registryURL, password string) func(*Config) {
	return func(cfg *Config) {
		cfg.StateConfig.RegistryCredentials.Username = username
		cfg.StateConfig.RegistryCredentials.URL = registryURL
		cfg.StateConfig.RegistryCredentials.Password = password
	}
}

func SetGroundControlURL(url string) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.GroundControlURL = url
	}
}

func SetLogLevel(level string) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.LogLevel = level
	}
}

func SetUseUnsecure(use bool) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.UseUnsecure = use
	}
}

func SetZotConfigPath(path string) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.ZotConfigPath = path
	}
}

func SetReplicationInterval(cronExpr string) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.StateReplicationInterval = cronExpr
	}
}

func SetUpdateInterval(cronExpr string) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.UpdateConfigInterval = cronExpr
	}
}

func SetStateReplicationInterval(cronExpr string) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.StateReplicationInterval = cronExpr
	}
}

func SetUpdateConfigInterval(cronExpr string) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.UpdateConfigInterval = cronExpr
	}
}

func SetRegisterSatelliteInterval(cronExpr string) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.RegisterSatelliteInterval = cronExpr
	}
}

func SetBringOwnRegistry(flag bool) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.BringOwnRegistry = flag
	}
}

func SetLocalRegistryCredentials(creds RegistryCredentials) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.LocalRegistryCredentials = creds
	}
}

func SetStateRegistryCredentials(creds RegistryCredentials) func(*Config) {
	return func(cfg *Config) {
		cfg.StateConfig.RegistryCredentials = creds
	}
}
