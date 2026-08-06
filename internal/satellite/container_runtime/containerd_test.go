package runtime

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRegistryCertsDir_Accepts covers registry names that must keep working.
func TestRegistryCertsDir_Accepts(t *testing.T) {
	defaultBase := "/etc/containerd/certs.d"

	tests := []struct {
		name     string
		base     string
		registry string
		want     string
	}{
		{"plain host", defaultBase, "docker.io", "/etc/containerd/certs.d/docker.io"},
		{"subdomain", defaultBase, "registry-1.docker.io", "/etc/containerd/certs.d/registry-1.docker.io"},
		{"host with port", defaultBase, "localhost:5000", "/etc/containerd/certs.d/localhost:5000"},
		{"ipv4 with port", defaultBase, "192.168.1.10:5000", "/etc/containerd/certs.d/192.168.1.10:5000"},
		{"ipv6 literal", defaultBase, "[::1]:5000", "/etc/containerd/certs.d/[::1]:5000"},
		{"unclean base is normalised", "/etc/containerd/certs.d/../certs.d", "docker.io", "/etc/containerd/certs.d/docker.io"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := registryCertsDir(tt.base, tt.registry)
			if err != nil {
				t.Fatalf("registryCertsDir(%q, %q) returned error: %v", tt.base, tt.registry, err)
			}
			if got != tt.want {
				t.Errorf("registryCertsDir(%q, %q) = %q, want %q", tt.base, tt.registry, got, tt.want)
			}
		})
	}
}

// TestRegistryCertsDir_RejectsTraversal is the regression test for the path
// traversal: a registry name supplied through Ground Control's config must never
// resolve to a directory outside certs.d, because the caller runs os.MkdirAll
// and os.Create on the result as root.
func TestRegistryCertsDir_RejectsTraversal(t *testing.T) {
	base := "/etc/containerd/certs.d"

	tests := []struct {
		name     string
		registry string
	}{
		{"parent traversal to cron.d", "../../../../etc/cron.d"},
		{"single parent segment", ".."},
		{"current directory", "."},
		{"absolute path", "/etc/cron.d"},
		{"embedded traversal", "docker.io/../../../etc"},
		{"trailing slash", "docker.io/"},
		{"nested path", "docker.io/nested"},
		{"backslash separator", `docker.io\..\..\etc`},
		{"https scheme", "https://docker.io"},
		{"http scheme", "http://docker.io"},
		{"empty", ""},
		{"whitespace only", "   "},
		{"leading whitespace", " docker.io"},
		{"trailing whitespace", "docker.io "},

		// These contain no path separator or ".." segment, so they resolve to a
		// directory inside base and previously passed here even though
		// config.ValidateRegistryName already rejected them. Now enforced by
		// this function too, since --mirrors reaches it without going through
		// that validation.
		{"double colon", "foo::bar"},
		{"unterminated ipv6", "[::1"},
		{"non numeric port", "localhost:abc"},
		{"port with leading plus", "docker.io:+443"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := registryCertsDir(base, tt.registry)
			if err == nil {
				t.Fatalf("registryCertsDir(%q, %q) = %q, want error", base, tt.registry, got)
			}
			if got != "" {
				t.Errorf("registryCertsDir returned %q alongside an error, want empty string", got)
			}
		})
	}
}

// TestRegistryCertsDir_StaysInsideBase asserts the containment property directly
// rather than relying on the rejection list above being exhaustive.
func TestRegistryCertsDir_StaysInsideBase(t *testing.T) {
	base := t.TempDir()
	prefix := filepath.Clean(base) + string(filepath.Separator)

	registries := []string{
		"docker.io",
		"../escape",
		"../../escape",
		"a/../../escape",
		"..",
		".",
		"/absolute",
		"docker.io/../..",
	}

	for _, registry := range registries {
		dir, err := registryCertsDir(base, registry)
		if err != nil {
			continue // rejected outright, which satisfies the property
		}
		if !strings.HasPrefix(dir, prefix) {
			t.Errorf("registryCertsDir(%q, %q) = %q, which escapes %q", base, registry, dir, base)
		}
	}
}
