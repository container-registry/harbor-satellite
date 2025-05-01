package models

import "github.com/container-registry/harbor-satellite/pkg/config"

// TODO: the satellite must now expect this state artifact
type SatelliteStateArtifact struct {
	States []string `json:"states,omitempty"`
	Config string   `json:"config,omitempty"`
}

type StateArtifact struct {
	Group     string     `json:"group,omitempty"`
	Registry  string     `json:"registry,omitempty"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
}

type ConfigObject struct {
	ConfigName string        `json:"config_name,omitempty"`
	Registry   string        `json:"registry,omitempty"`
	Config     config.Config `json:"config,omitempty"`
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
