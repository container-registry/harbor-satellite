package runtime

// RegistriesConf models fields of /etc/containers/registries.conf
type RegistriesConf struct {
	Registries []Registry             `mapstructure:"registry" toml:"registry"`
	Unknown    map[string]interface{} `mapstructure:",remain"` // do not touch parts we do not deal with
}

// Registry models registry field in /etc/containers/registries.conf
type Registry struct {
	Location string   `mapstructure:"location" toml:"location"`
	Mirrors  []Mirror `mapstructure:"mirror,omitempty" toml:"mirror,omitempty"`
}

// Mirror models mirror field in /etc/containers/registries.conf
type Mirror struct {
	Location string `mapstructure:"location" toml:"location"`
	Insecure bool   `mapstructure:"insecure" toml:"insecure"`
}
