package models

type SatelliteStateArtifact struct {
	States []string `json:"states,omitempty"`
}

type StateArtifact struct {
	Group     string     `json:"group,omitempty"`
	Registry  string     `json:"registry,omitempty"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
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
	States []string `json:"states"`
	Auth   Account  `json:"auth"`
}

type Account struct {
	Name     string `json:"name"`
	Secret   string `json:"secret"`
	Registry string `json:"registry"`
}
