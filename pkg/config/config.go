package config

import (
	"encoding/json"
	"github.com/rs/zerolog"
)

type URL string

type RegistryCredentials struct {
	URL      `json:"url,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type AppConfig struct {
	GroundControlURL          URL                 `json:"ground_control_url"`
	LogLevel                  string              `json:"log_level,omitempty"`
	UseUnsecure               bool                `json:"use_unsecure,omitempty"`
	StateReplicationInterval  string              `json:"state_replication_interval,omitempty"`
	UpdateConfigInterval      string              `json:"update_config_interval,omitempty"`
	RegisterSatelliteInterval string              `json:"register_satellite_interval,omitempty"`
	BringOwnRegistry          bool                `json:"bring_own_registry,omitempty"`
	LocalRegistryCredentials  RegistryCredentials `json:"local_registry"`
}

// TODO: Might need to update ground control code for this to work.
type StateConfig struct {
	RegistryCredentials RegistryCredentials `json:"auth,omitempty"`
	StateURL            string              `json:"state,omitempty"`
}

type Config struct {
	StateConfig  StateConfig     `json:"state_config,omitempty"`
	AppConfig    AppConfig       `json:"app_config,omitempty"`
	ZotConfigRaw json.RawMessage `json:"zot_config"`
}

var validLogLevels = map[string]bool{
	zerolog.LevelDebugValue: true,
	zerolog.LevelInfoValue:  true,
	zerolog.LevelWarnValue:  true,
	zerolog.LevelErrorValue: true,
	zerolog.LevelFatalValue: true,
	zerolog.LevelPanicValue: true,
}
