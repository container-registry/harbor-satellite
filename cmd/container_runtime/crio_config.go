package runtime

// CriORegistryConfig represents the overall configuration for container image registries.
type CriORegistryConfig struct {
    // UnqualifiedSearchRegistries is an array of host[:port] registries to try
    // when pulling an unqualified image, in the specified order.
    UnqualifiedSearchRegistries []string `toml:"unqualified-search-registries,omitempty"`

    // Registries is a list of registry configurations, each defining the behavior for a specific prefix or namespace.
    Registries []Registry `toml:"registry,omitempty"`
}

// Registry represents a specific registry configuration.
type Registry struct {
    // Prefix is used to choose the relevant [[registry]] TOML table.
    // Only the table with the longest match for the input image name
    // (considering namespace/repo/tag/digest separators) is used.
    // If this field is missing, it defaults to the value of Location.
    // Example: "example.com/foo"
    Prefix string `toml:"prefix,omitempty"`

    // Insecure allows unencrypted HTTP as well as TLS connections with untrusted certificates
    // if set to true. This should only be enabled for trusted registries to avoid security risks.
    Insecure bool `toml:"insecure,omitempty"`

    // Blocked, if set to true, prevents pulling images with matching names from this registry.
    // This can be used to blacklist certain registries.
    Blocked bool `toml:"blocked,omitempty"`

    // Location specifies the physical location of the "prefix"-rooted namespace.
    // By default, this is equal to "prefix". It can be empty for wildcarded prefixes (e.g., "*.example.com"),
    // in which case the input reference is used as-is without modification.
    // Example: "internal-registry-for-example.com/bar"
    Location string `toml:"location,omitempty"`

    // Mirrors is an array of potential mirror locations for the "prefix"-rooted namespace.
    // Mirrors are attempted in the specified order; the first reachable mirror containing the image
    // is used. If no mirror has the image, the primary location or the unmodified user-specified reference is tried last.
    Mirrors []Mirror `toml:"mirror,omitempty"`
}

// Mirror represents a mirror registry configuration.
type Mirror struct {
    // Location specifies the address of the mirror. The mirror will be used if it contains the image.
    // Example: "example-mirror-0.local/mirror-for-foo"
    Location string `toml:"location,omitempty"`

    // Insecure allows access to the mirror over unencrypted HTTP or with untrusted TLS certificates
    // if set to true. This should be used cautiously.
    Insecure bool `toml:"insecure,omitempty"`
}
