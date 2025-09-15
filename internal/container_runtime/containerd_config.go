package runtime

// Host represents a registry host entry in a hosts.toml file
type Host struct {
	Capabilities []string `toml:"capabilities"`
}

// ContainerdHosts represents the structure of a hosts.toml file
type ContainerdHosts struct {
	Server string          `toml:"server"`
	Host   map[string]Host `toml:"host"`
}
