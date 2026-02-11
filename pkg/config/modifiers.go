package config

import (
	"encoding/json"
	"fmt"
	"net/url"
)

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

func SetZotConfigRaw(raw json.RawMessage) func(*Config) {
	return func(cfg *Config) {
		cfg.ZotConfigRaw = raw
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

func SetRegistryFallbackConfig(fb RegistryFallbackConfig) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.RegistryFallback = fb
	}
}

func SetHarborRegistryURL(url string) func(*Config) {
	return func(cfg *Config) {
		cfg.AppConfig.HarborRegistryURL = url
	}
}

// ApplyHarborRegistryOverride replaces the scheme and host:port in both auth URL
// and state URL with the provided override. Used when GC returns a Docker-internal
// address (e.g., host.docker.internal:8080) that doesn't resolve on bare-metal nodes.
func ApplyHarborRegistryOverride(sc StateConfig, override string) (StateConfig, error) {
	overrideParsed, err := url.Parse(override)
	if err != nil {
		return sc, fmt.Errorf("parse harbor registry override URL: %w", err)
	}

	replaceHost := func(raw string) (string, error) {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("parse URL %q: %w", raw, err)
		}
		parsed.Scheme = overrideParsed.Scheme
		parsed.Host = overrideParsed.Host
		return parsed.String(), nil
	}

	authURL := string(sc.RegistryCredentials.URL)
	if authURL != "" {
		newAuth, err := replaceHost(authURL)
		if err != nil {
			return sc, err
		}
		sc.RegistryCredentials.URL = URL(newAuth)
	}

	if sc.StateURL != "" {
		newState, err := replaceHost(sc.StateURL)
		if err != nil {
			return sc, err
		}
		sc.StateURL = newState
	}

	return sc, nil
}
