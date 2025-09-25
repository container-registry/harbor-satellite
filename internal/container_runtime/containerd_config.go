package runtime

// Host represents a registry host entry in a hosts.toml file
type Host struct {
	Capabilities []string `toml:"capabilities"`
}

// ContainerdHosts represents the structure of a hosts.toml file
type ContainerdHosts struct {
	Server  string                 `toml:"server"`
	Host    map[string]Host        `toml:"host"`
	Unknown map[string]interface{} `toml:",remain"` // do not touch parts we do not deal with
}

type ContainerdConfig struct {
	Plugins *Plugins `toml:"plugins,omitempty"`
}

type Plugins struct {
	CRIImages *CRIImages `toml:"io.containerd.cri.v1.images,omitempty"`
}

type CRIImages struct {
	Registry *RegistryPlugin `toml:"registry,omitempty"`
}

type RegistryPlugin struct {
	ConfigPath string `toml:"config_path"`
}
