package runtime

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
