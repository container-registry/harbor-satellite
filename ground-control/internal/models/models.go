package models

import "github.com/container-registry/harbor-satellite/pkg/config"

// SatelliteStateArtifact describes a satellite state artifact.
//
// swagger:model SatelliteStateArtifact
type SatelliteStateArtifact struct {
	States []string `json:"states,omitempty"`
	Config string   `json:"config,omitempty"`
}

// StateArtifact describes a group state artifact synchronized from Harbor.
//
// swagger:model StateArtifact
type StateArtifact struct {
	Group     string     `json:"group,omitempty"`
	Registry  string     `json:"registry,omitempty"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
}

// ConfigObject wraps a named satellite configuration.
//
// swagger:model ConfigObject
type ConfigObject struct {
	ConfigName string        `json:"config_name,omitempty"`
	Registry   string        `json:"registry,omitempty"`
	Config     config.Config `json:"config,omitempty"`
}

// Artifact describes an image artifact in a group state.
//
// swagger:model Artifact
type Artifact struct {
	Repository string   `json:"repository,omitempty"`
	Tag        []string `json:"tag,omitempty"`
	Labels     any      `json:"labels,omitempty"`
	Type       string   `json:"type,omitempty"`
	Digest     string   `json:"digest,omitempty"`
	Deleted    bool     `json:"deleted,omitempty"`
}

// ZtrResult contains state and registry auth returned during ZTR.
//
// swagger:model ZtrResult
type ZtrResult struct {
	State string                     `json:"state"`
	Auth  config.RegistryCredentials `json:"auth"`
}

// Account contains registry account credentials.
//
// swagger:model Account
type Account struct {
	Name     string `json:"name"`
	Secret   string `json:"secret"`
	Registry string `json:"registry"`
}
