package runtime

// ContainerdConfigToml provides containerd configuration data for the server
type ContainerdConfigToml struct {
	// Version of the config file
	Version int `toml:"version,omitempty"`
	// Root is the path to a directory where containerd will store persistent data
	Root string `toml:"root,omitempty"`
	// State is the path to a directory where containerd will store transient data
	State string `toml:"state,omitempty"`
	// TempDir is the path to a directory where to place containerd temporary files
	TempDir string `toml:"temp,omitempty"`
	// PluginDir is the directory for dynamic plugins to be stored
	//
	// Deprecated: Please use proxy or binary external plugins.
	PluginDir string `toml:"plugin_dir,omitempty"`
	// GRPC configuration settings
	GRPC GRPCConfig `toml:"grpc,omitempty"`
	// TTRPC configuration settings
	TTRPC TTRPCConfig `toml:"ttrpc,omitempty"`
	// Debug and profiling settings
	Debug Debug `toml:"debug,omitempty"`
	// Metrics and monitoring settings
	Metrics MetricsConfig `toml:"metrics,omitempty"`
	// DisabledPlugins are IDs of plugins to disable. Disabled plugins won't be
	// initialized and started.
	// DisabledPlugins must use a fully qualified plugin URI.
	DisabledPlugins []string `toml:"disabled_plugins,omitempty"`
	// RequiredPlugins are IDs of required plugins. Containerd exits if any
	// required plugin doesn't exist or fails to be initialized or started.
	// RequiredPlugins must use a fully qualified plugin URI.
	RequiredPlugins []string `toml:"required_plugins,omitempty"`
	// Plugins provides plugin specific configuration for the initialization of a plugin
	Plugins PluginsConfig `toml:"plugins,omitempty"`
	// OOMScore adjust the containerd's oom score
	OOMScore int `toml:"oom_score,omitempty"`
	// Cgroup specifies cgroup information for the containerd daemon process
	Cgroup CgroupConfig `toml:"cgroup,omitempty"`
	// ProxyPlugins configures plugins which are communicated to over GRPC
	ProxyPlugins map[string]ProxyPlugin `toml:"proxy_plugins,omitempty"`
	// Timeouts specified as a duration
	Timeouts map[string]string `toml:"timeouts,omitempty"`
	// Imports are additional file path list to config files that can overwrite main config file fields
	Imports []string `toml:"imports,omitempty"`
	// StreamProcessors configuration
	StreamProcessors map[string]StreamProcessor `toml:"stream_processors,omitempty"`
}

type StreamProcessor struct {
	// Accepts specific media-types
	Accepts []string `toml:"accepts,omitempty"`
	// Returns the media-type
	Returns string `toml:"returns,omitempty"`
	// Path or name of the binary
	Path string `toml:"path"`
	// Args to the binary
	Args []string `toml:"args,omitempty"`
	// Environment variables for the binary
	Env []string `toml:"env,omitempty"`
}

type GRPCConfig struct {
	Address        string `toml:"address"`
	TCPAddress     string `toml:"tcp_address,omitempty"`
	TCPTLSCA       string `toml:"tcp_tls_ca,omitempty"`
	TCPTLSCert     string `toml:"tcp_tls_cert,omitempty"`
	TCPTLSKey      string `toml:"tcp_tls_key,omitempty"`
	UID            int    `toml:"uid,omitempty"`
	GID            int    `toml:"gid,omitempty"`
	MaxRecvMsgSize int    `toml:"max_recv_message_size,omitempty"`
	MaxSendMsgSize int    `toml:"max_send_message_size,omitempty"`
}

// TTRPCConfig provides TTRPC configuration for the socket
type TTRPCConfig struct {
	Address string `toml:"address"`
	UID     int    `toml:"uid,omitempty"`
	GID     int    `toml:"gid,omitempty"`
}

// Debug provides debug configuration
type Debug struct {
	Address string `toml:"address,omitempty"`
	UID     int    `toml:"uid,omitempty"`
	GID     int    `toml:"gid,omitempty"`
	Level   string `toml:"level,omitempty"`
	// Format represents the logging format. Supported values are 'text' and 'json'.
	Format string `toml:"format,omitempty"`
}

// MetricsConfig provides metrics configuration
type MetricsConfig struct {
	Address       string `toml:"address,omitempty"`
	GRPCHistogram bool   `toml:"grpc_histogram,omitempty"`
}

// CgroupConfig provides cgroup configuration
type CgroupConfig struct {
	Path string `toml:"path,omitempty"`
}

// ProxyPlugin provides a proxy plugin configuration
type ProxyPlugin struct {
	Type         string            `toml:"type"`
	Address      string            `toml:"address"`
	Platform     string            `toml:"platform,omitempty"`
	Exports      map[string]string `toml:"exports,omitempty"`
	Capabilities []string          `toml:"capabilities,omitempty"`
}

type PluginsConfig struct {
	Cri          CriConfig           `toml:"io.containerd.grpc.v1.cri,omitempty"`
	Cgroups      MonitorConfig       `toml:"io.containerd.monitor.v1.cgroups,omitempty"`
	LinuxRuntime interface{}         `toml:"io.containerd.runtime.v1.linux,omitempty"`
	Scheduler    GCSchedulerConfig   `toml:"io.containerd.gc.v1.scheduler,omitempty"`
	Bolt         interface{}         `toml:"io.containerd.metadata.v1.bolt,omitempty"`
	Task         RuntimeV2TaskConfig `toml:"io.containerd.runtime.v2.task,omitempty"`
	Opt          interface{}         `toml:"io.containerd.internal.v1.opt,omitempty"`
	Restart      interface{}         `toml:"io.containerd.internal.v1.restart,omitempty"`
	Tracing      interface{}         `toml:"io.containerd.internal.v1.tracing,omitempty"`
	Otlp         interface{}         `toml:"io.containerd.tracing.processor.v1.otlp,omitempty"`
	Aufs         interface{}         `toml:"io.containerd.snapshotter.v1.aufs,omitempty"`
	Btrfs        interface{}         `toml:"io.containerd.snapshotter.v1.btrfs,omitempty"`
	Devmapper    interface{}         `toml:"io.containerd.snapshotter.v1.devmapper,omitempty"`
	Native       interface{}         `toml:"io.containerd.snapshotter.v1.native,omitempty"`
	Overlayfs    interface{}         `toml:"io.containerd.snapshotter.v1.overlayfs,omitempty"`
	Zfs          interface{}         `toml:"io.containerd.snapshotter.v1.zfs,omitempty"`
}

type MonitorConfig struct {
	NoPrometheus bool `toml:"no_prometheus,omitempty"`
}

type GCSchedulerConfig struct {
	PauseThreshold    float64 `toml:"pause_threshold,omitempty"`
	DeletionThreshold int     `toml:"deletion_threshold,omitempty"`
	MutationThreshold int     `toml:"mutation_threshold,omitempty"`
	ScheduleDelay     string  `toml:"schedule_delay,omitempty"`
	StartupDelay      string  `toml:"startup_delay,omitempty"`
}

type RuntimeV2TaskConfig struct {
	Platforms []string `toml:"platforms,omitempty"`
	SchedCore bool     `toml:"sched_core,omitempty"`
}

type CriConfig struct {
	Containerd CriContainerdConfig `toml:"containerd,omitempty"`
	Registry   RegistryConfig      `toml:"registry,omitempty"`
}

type CriContainerdConfig struct {
	DefaultRuntimeName string                   `toml:"default_runtime_name,omitempty"`
	Runtimes           map[string]RuntimeConfig `toml:"runtimes,omitempty"`
}

type RuntimeConfig struct {
	PrivilegedWithoutHostDevices bool           `toml:"privileged_without_host_devices,omitempty"`
	RuntimeType                  string         `toml:"runtime_type"`
	Options                      RuntimeOptions `toml:"options,omitempty"`
}

type RuntimeOptions struct {
	BinaryName string `toml:"BinaryName,omitempty"`
}

type RegistryConfig struct {
	ConfigPath string `toml:"config_path,omitempty"`
}