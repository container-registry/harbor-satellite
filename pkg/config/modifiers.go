package config

// Threadsafe setter functions to modify config data.

func SetStateURL(url string) func(*Config) {
	return func(cfg *Config) {
		cfg.StateConfig.StateURL = url
	}
}

func SetStateAuth(username, password string, registryURL URL) func(*Config) {
	return func(cfg *Config) {
		cfg.StateConfig.RegistryCredentials.Username = username
		cfg.StateConfig.RegistryCredentials.URL = registryURL
		cfg.StateConfig.RegistryCredentials.Password = password
	}
}

func SetStateConfig(sc StateConfig) func(*Config) {
	return func(cfg *Config) {
		cfg.StateConfig = sc
	}
}

func SetGroundControlURL(url string) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.GroundControlURL = URL(url)
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

func SetStateReplicationInterval(cronExpr string) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.StateReplicationInterval = cronExpr
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

func SetLocalRegistryURL(url string) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.LocalRegistryCredentials.URL = URL(url)
	}
}

func SetLocalRegistryUsername(username string) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.LocalRegistryCredentials.Username = username
	}
}

func SetLocalRegistryPassword(password string) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.LocalRegistryCredentials.Password = password
	}
}

func SetLocalRegistryCredentials(creds RegistryCredentials) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.LocalRegistryCredentials = creds
	}
}

func SetSPIFFEConfig(spiffeCfg SPIFFEConfig) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.SPIFFE = spiffeCfg
	}
}

func SetSPIFFEEnabled(enabled bool) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.SPIFFE.Enabled = enabled
	}
}
