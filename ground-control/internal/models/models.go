package models

type SatelliteStateArtifact struct {
	States []string `json:"states,omitempty"`
}

type StateArtifact struct {
	Group     string     `json:"group,omitempty"`
	Registry  string     `json:"registry,omitempty"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
}

//TODO: move config to a common pkg for both satellite and ground control
type LocalRegistryConfig struct {
	URL              string `json:"url"`
	UserName         string `json:"username"`
	Password         string `json:"password"`
	BringOwnRegistry bool   `json:"bring_own_registry"`
}

// LocalJsonConfig is a struct that holds the configs that are passed as environment variables
type LocalJsonConfig struct {
	GroundControlURL          string              `json:"ground_control_url"`
	LogLevel                  string              `json:"log_level"`
	UseUnsecure               bool                `json:"use_unsecure"`
	ZotConfigPath             string              `json:"zot_config_path"`
	StateReplicationInterval  string              `json:"state_replication_interval"`
	UpdateConfigInterval      string              `json:"update_config_interval"`
	RegisterSatelliteInterval string              `json:"register_satellite_interval"`
	LocalRegistryConfig       LocalRegistryConfig `json:"local_registry"`
}

type Artifact struct {
	Repository string   `json:"repository,omitempty"`
	Tag        []string `json:"tag,omitempty"`
	Labels     any      `json:"labels,omitempty"`
	Type       string   `json:"type,omitempty"`
	Digest     string   `json:"digest,omitempty"`
	Deleted    bool     `json:"deleted,omitempty"`
}

type ZtrResult struct {
	State string  `json:"state"`
	Auth  Account `json:"auth"`
}

type Account struct {
	Name     string `json:"name"`
	Secret   string `json:"secret"`
	Registry string `json:"registry"`
}
