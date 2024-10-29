package models

type StateArtifact struct {
	Group     string     `json:"group"`
	Registry  string     `json:"registry"`
	Artifacts []Artifact `json:"artifacts"`
}

type Artifact struct {
	Repository string   `json:"repository"`
	Tag        []string `json:"tag"`
	Labels     any      `json:"labels"`
	Type       string   `json:"type"`
	Digest     string   `json:"digest"`
	Deleted    bool     `json:"deleted"`
}

type ZtrResult struct {
	States []string `json:"states"`
	Auth   Account  `json:"auth"`
}

type Account struct {
	Name   string `json:"name"`
	Secret string `json:"secret"`
}

