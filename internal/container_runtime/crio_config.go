package runtime

// RegistriesConf models the structure of config relevant for registries
type RegistriesConf struct {
	Registries []Registry `toml:"registry"`
}

type Registry struct {
	Location string   `toml:"location"`
	Mirrors  []Mirror `toml:"mirror,omitempty"`
}

type Mirror struct {
	Location string `toml:"location"`
	Insecure bool   `toml:"insecure,omitempty"`
}
