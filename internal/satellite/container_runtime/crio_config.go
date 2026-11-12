package runtime

// RegistriesConf models fields of /etc/containers/registries.conf
type RegistriesConf struct {
	Registries []Registry     `mapstructure:"registry" toml:"registry"`
	Unknown    map[string]any `mapstructure:",remain"` // do not touch parts we do not deal with
}

// Registry models registry field in /etc/containers/registries.conf.
// Other holds the keys the satellite does not manage, such as prefix,
// insecure and blocked, so a rewrite keeps them.
type Registry struct {
	Location string         `mapstructure:"location"`
	Mirrors  []Mirror       `mapstructure:"mirror,omitempty"`
	Other    map[string]any `mapstructure:",remain"`
}

// Mirror models mirror field in /etc/containers/registries.conf; Other
// keeps keys such as pull-from-mirror.
type Mirror struct {
	Location string         `mapstructure:"location"`
	Insecure bool           `mapstructure:"insecure"`
	Other    map[string]any `mapstructure:",remain"`
}

// tomlTable is the registry as written back, its other keys included.
func (r Registry) tomlTable() map[string]any {
	table := make(map[string]any, len(r.Other)+2)
	for k, v := range r.Other {
		table[k] = v
	}
	table["location"] = r.Location
	if len(r.Mirrors) > 0 {
		mirrors := make([]map[string]any, 0, len(r.Mirrors))
		for _, m := range r.Mirrors {
			mirror := make(map[string]any, len(m.Other)+2)
			for k, v := range m.Other {
				mirror[k] = v
			}
			mirror["location"] = m.Location
			mirror["insecure"] = m.Insecure
			mirrors = append(mirrors, mirror)
		}
		table["mirror"] = mirrors
	}
	return table
}
