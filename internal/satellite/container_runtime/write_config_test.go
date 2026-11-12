package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/require"
)

const proxy = "127.0.0.1:8585"

// inTempEtc points every CRI config path at a fresh directory.
func inTempEtc(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	saved := []string{containerdCertsDir, containerdConfigPath, registriesConfigPath, tempRegistriesConfigPath, dockerConfigPath}
	savedRestart := restartDocker
	t.Cleanup(func() {
		containerdCertsDir, containerdConfigPath, registriesConfigPath, tempRegistriesConfigPath, dockerConfigPath = saved[0], saved[1], saved[2], saved[3], saved[4]
		restartDocker = savedRestart
	})
	containerdCertsDir = filepath.Join(dir, "containerd", "certs.d")
	containerdConfigPath = filepath.Join(dir, "containerd", "config.toml")
	registriesConfigPath = filepath.Join(dir, "containers", "registries.conf")
	tempRegistriesConfigPath = filepath.Join(dir, "containers", "registries.toml")
	dockerConfigPath = filepath.Join(dir, "docker", "daemon.json")
	for _, d := range []string{"containerd", "containers", "docker"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, d), 0o750))
	}
}

func decodeTOML(t *testing.T, path string) map[string]any {
	t.Helper()
	var m map[string]any
	_, err := toml.DecodeFile(path, &m)
	require.NoError(t, err)
	return m
}

func registries(t *testing.T) []map[string]any {
	t.Helper()
	r, ok := decodeTOML(t, registriesConfigPath)["registry"].([]map[string]any)
	require.True(t, ok, "registries.conf has no [[registry]] tables")
	return r
}

func dig(t *testing.T, m map[string]any, keys ...string) any {
	t.Helper()
	var cur any = m
	for _, k := range keys {
		next, ok := cur.(map[string]any)
		require.True(t, ok, "%v is not a table at %q", cur, k)
		cur = next[k]
	}
	return cur
}

func TestContainerdConfigOnAFreshHost(t *testing.T) {
	inTempEtc(t)

	backup, err := setContainerdConfig([]string{"docker.io", "quay.io"}, proxy)
	require.NoError(t, err)
	require.Empty(t, backup, "there was nothing to back up")

	cfg := decodeTOML(t, containerdConfigPath)
	require.Equal(t, containerdCertsDir, dig(t, cfg, "plugins", "io.containerd.cri.v1.images", "registry", "config_path"))

	for dir, server := range map[string]string{"docker.io": "https://docker.io", "quay.io": "https://quay.io"} {
		hosts := decodeTOML(t, filepath.Join(containerdCertsDir, dir, "hosts.toml"))
		require.Equal(t, server, hosts["server"], dir)
		require.Equal(t, []any{"pull", "resolve"}, dig(t, hosts, "host", "http://"+proxy, "capabilities"), dir)
	}
}

func TestContainerdConfigKeepsTheRestOfConfigToml(t *testing.T) {
	inTempEtc(t)
	original := `version = 3
root = "/var/lib/containerd"

[plugins."io.containerd.cri.v1.images"]
  snapshotter = "overlayfs"

[plugins."io.containerd.cri.v1.runtime".containerd]
  default_runtime_name = "runc"
`
	require.NoError(t, os.WriteFile(containerdConfigPath, []byte(original), 0o600))

	backup, err := setContainerdConfig([]string{"docker.io"}, proxy)
	require.NoError(t, err)
	saved, err := os.ReadFile(backup)
	require.NoError(t, err)
	require.Equal(t, original, string(saved), "the backup holds the original")

	cfg := decodeTOML(t, containerdConfigPath)
	require.Equal(t, int64(3), cfg["version"])
	require.Equal(t, "/var/lib/containerd", cfg["root"])
	require.Equal(t, "overlayfs", dig(t, cfg, "plugins", "io.containerd.cri.v1.images", "snapshotter"))
	require.Equal(t, "runc", dig(t, cfg, "plugins", "io.containerd.cri.v1.runtime", "containerd", "default_runtime_name"))
	require.Equal(t, containerdCertsDir, dig(t, cfg, "plugins", "io.containerd.cri.v1.images", "registry", "config_path"))
}

func TestContainerdConfigRefusesABrokenConfigToml(t *testing.T) {
	inTempEtc(t)
	require.NoError(t, os.WriteFile(containerdConfigPath, []byte("version = [broken"), 0o600))
	_, err := setContainerdConfig([]string{"docker.io"}, proxy)
	require.ErrorContains(t, err, "failed to parse")
	data, err := os.ReadFile(containerdConfigPath)
	require.NoError(t, err)
	require.Equal(t, "version = [broken", string(data), "a config it cannot parse is left alone")
}

func TestContainerdHostsTomlKeepsOtherHosts(t *testing.T) {
	inTempEtc(t)
	dir := filepath.Join(containerdCertsDir, "docker.io")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hosts.toml"), []byte(`server = "https://registry-1.docker.io"

[host."https://mirror.example"]
  capabilities = ["pull"]
`), 0o600))

	_, err := setContainerdConfig([]string{"docker.io"}, "https://satellite.local:8585")
	require.NoError(t, err)

	hosts := decodeTOML(t, filepath.Join(dir, "hosts.toml"))
	require.Equal(t, "https://docker.io", hosts["server"])
	require.Equal(t, []any{"pull"}, dig(t, hosts, "host", "https://mirror.example", "capabilities"))
	require.Equal(t, []any{"pull", "resolve"}, dig(t, hosts, "host", "https://satellite.local:8585", "capabilities"))
}

func TestCrioConfigAddsTheProxyAsMirror(t *testing.T) {
	inTempEtc(t)
	require.NoError(t, os.WriteFile(registriesConfigPath, []byte(`unqualified-search-registries = ["docker.io"]

[[registry]]
location = "docker.io"

[[registry.mirror]]
location = "mirror.example"
insecure = false
`), 0o600))

	backup, err := setCrioConfig([]string{"docker.io", "quay.io"}, proxy)
	require.NoError(t, err)
	require.FileExists(t, backup)
	require.NoFileExists(t, tempRegistriesConfigPath, "the temporary copy is removed")

	cfg := decodeTOML(t, registriesConfigPath)
	require.Equal(t, []any{"docker.io"}, cfg["unqualified-search-registries"])
	regs := registries(t)
	require.Len(t, regs, 2)
	require.Equal(t, "docker.io", regs[0]["location"])
	require.Equal(t, []map[string]any{
		{"location": "mirror.example", "insecure": false},
		{"location": proxy, "insecure": true},
	}, regs[0]["mirror"])
	require.Equal(t, "quay.io", regs[1]["location"])
	require.Equal(t, []map[string]any{{"location": proxy, "insecure": true}}, regs[1]["mirror"])

	_, err = setCrioConfig([]string{"docker.io"}, proxy)
	require.NoError(t, err)
	require.Len(t, registries(t)[0]["mirror"], 2, "running twice does not add the proxy twice")
}

func TestCrioConfigCreatesRegistriesConf(t *testing.T) {
	inTempEtc(t)
	backup, err := setCrioConfig([]string{"docker.io"}, "https://satellite.local")
	require.NoError(t, err)
	require.FileExists(t, backup, "the empty file it created is backed up")
	require.Equal(t, []map[string]any{{"location": "https://satellite.local", "insecure": false}}, registries(t)[0]["mirror"])
}

func readDocker(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(dockerConfigPath)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}

func TestDockerConfigAddsTheProxyAsRegistryMirror(t *testing.T) {
	inTempEtc(t)
	restarts := 0
	restartDocker = func(context.Context) error { restarts++; return nil }
	require.NoError(t, os.WriteFile(dockerConfigPath, []byte(`{"log-driver":"journald","registry-mirrors":["https://mirror.example"]}`), 0o600))

	backup, err := setDockerdConfig([]string{"true"}, proxy)
	require.NoError(t, err)
	require.FileExists(t, backup)
	cfg := readDocker(t)
	require.Equal(t, "journald", cfg["log-driver"])
	require.Equal(t, []any{"https://mirror.example", "http://" + proxy}, cfg["registry-mirrors"])
	require.Equal(t, 1, restarts)

	_, err = setDockerdConfig([]string{"true"}, proxy)
	require.NoError(t, err)
	require.Len(t, readDocker(t)["registry-mirrors"], 2, "running twice does not add the proxy twice")
}

func TestDockerConfigOnAFreshHost(t *testing.T) {
	inTempEtc(t)
	restartDocker = func(context.Context) error { return nil }
	backup, err := setDockerdConfig([]string{"true"}, "http://"+proxy)
	require.NoError(t, err)
	require.Empty(t, backup)
	require.Equal(t, []any{"http://" + proxy}, readDocker(t)["registry-mirrors"])
}

func TestDockerConfigRollsBackWhenTheRestartFails(t *testing.T) {
	inTempEtc(t)
	original := `{"registry-mirrors":["https://mirror.example"]}`
	require.NoError(t, os.WriteFile(dockerConfigPath, []byte(original), 0o600))
	restartDocker = func(context.Context) error { return errors.New("unit docker.service not found") }

	_, err := setDockerdConfig([]string{"true"}, proxy)
	require.ErrorContains(t, err, "rolled back config")
	data, err := os.ReadFile(dockerConfigPath)
	require.NoError(t, err)
	require.Equal(t, original, string(data))
}

func TestDockerConfigFlag(t *testing.T) {
	inTempEtc(t)
	restartDocker = func(context.Context) error { t.Fatal("docker restarted"); return nil }
	for _, mirrors := range [][]string{nil, {"false"}} {
		backup, err := setDockerdConfig(mirrors, proxy)
		require.NoError(t, err)
		require.Empty(t, backup)
	}
	_, err := setDockerdConfig([]string{"docker.io"}, proxy)
	require.ErrorContains(t, err, "docker mirror must be true/false")
	require.NoFileExists(t, dockerConfigPath)
}

// Settings of a registry the satellite does not manage stay as they were.
func TestCrioConfigKeepsTheRegistrySettings(t *testing.T) {
	inTempEtc(t)
	require.NoError(t, os.WriteFile(registriesConfigPath, []byte(`[[registry]]
prefix = "docker.io"
location = "docker.io"
insecure = true
blocked = false
mirror-by-digest-only = true

[[registry.mirror]]
location = "mirror.example"
pull-from-mirror = "digest-only"
`), 0o600))

	_, err := setCrioConfig([]string{"docker.io"}, proxy)
	require.NoError(t, err)

	got := registries(t)
	require.Len(t, got, 1)
	for key, want := range map[string]any{"prefix": "docker.io", "insecure": true, "blocked": false, "mirror-by-digest-only": true} {
		require.Equal(t, want, got[0][key], key)
	}
	require.Equal(t, []map[string]any{
		{"location": "mirror.example", "insecure": false, "pull-from-mirror": "digest-only"},
		{"location": proxy, "insecure": true},
	}, got[0]["mirror"])
}

// Settings of hosts.toml the satellite does not manage stay as they were.
func TestContainerdHostsTomlKeepsHostSettings(t *testing.T) {
	inTempEtc(t)
	dir := filepath.Join(containerdCertsDir, "registry.example")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hosts.toml"), []byte(`server = "https://registry.example"
capabilities = ["pull", "resolve", "push"]
ca = "/etc/certs/root.pem"

[host."https://mirror.example"]
  capabilities = ["pull"]
  skip_verify = true
  override_path = true
  [host."https://mirror.example".header]
    x-custom = ["1"]
`), 0o600))

	_, err := setContainerdConfig([]string{"registry.example"}, proxy)
	require.NoError(t, err)

	hosts := decodeTOML(t, filepath.Join(dir, "hosts.toml"))
	require.Equal(t, []any{"pull", "resolve", "push"}, hosts["capabilities"])
	require.Equal(t, "/etc/certs/root.pem", hosts["ca"])
	require.Equal(t, true, dig(t, hosts, "host", "https://mirror.example", "skip_verify"))
	require.Equal(t, true, dig(t, hosts, "host", "https://mirror.example", "override_path"))
	require.Equal(t, []any{"1"}, dig(t, hosts, "host", "https://mirror.example", "header", "x-custom"))
}

// containerd reads certs.d/<host[:port]>/hosts.toml; a registry given with
// its scheme must land there, not in certs.d/https:/<host>.
func TestContainerdHostsDirIsTheRegistryHost(t *testing.T) {
	inTempEtc(t)
	_, err := setContainerdConfig([]string{"https://quay.io", "http://registry.local:5000/"}, proxy)
	require.NoError(t, err)

	require.Equal(t, "https://quay.io", decodeTOML(t, filepath.Join(containerdCertsDir, "quay.io", "hosts.toml"))["server"])
	require.Equal(t, "http://registry.local:5000/", decodeTOML(t, filepath.Join(containerdCertsDir, "registry.local:5000", "hosts.toml"))["server"])
	require.NoDirExists(t, filepath.Join(containerdCertsDir, "https:"))
	require.NoDirExists(t, filepath.Join(containerdCertsDir, "http:"))
}

func TestContainerdRefusesARegistryOutsideCertsD(t *testing.T) {
	inTempEtc(t)
	for _, registry := range []string{"../../escape", "..", "https://", "a/b"} {
		_, err := setContainerdConfig([]string{registry}, proxy)
		require.ErrorContains(t, err, "is not a host[:port]", registry)
	}
	require.NoFileExists(t, filepath.Join(filepath.Dir(filepath.Dir(containerdCertsDir)), "escape", "hosts.toml"))
}
