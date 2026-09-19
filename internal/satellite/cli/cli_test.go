package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

func testOptions() *SatelliteOptions {
	return &SatelliteOptions{
		JSONLogging:          true,
		ProxyMode:            proxy.ModeProxy,
		ProxyPort:            config.DefaultProxyPort,
		SPIFFEEndpointSocket: config.DefaultSPIFFEEndpointSocket,
		ShutdownTimeout:      "30s",
	}
}

func writeConfigFile(t *testing.T, dir string, cfg config.Config) {
	t.Helper()
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600))
}

func standaloneStateConfig() config.StateConfig {
	return config.StateConfig{
		RegistryCredentials: config.RegistryCredentials{
			URL:      "https://harbor.example.com",
			Username: "robot$satellite",
			Password: "secret",
		},
		StateURL: "https://harbor.example.com/satellite/state/example:latest",
	}
}

func TestRootCommandShowsHelpWithoutStarting(t *testing.T) {
	root := RootCmd()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{})

	require.NoError(t, root.Execute())
	require.Contains(t, output.String(), "serve")
	require.Contains(t, output.String(), "configure")
}

func TestFallbackOnlyFlagWasRemoved(t *testing.T) {
	root := RootCmd()
	root.SetArgs([]string{"serve", "--fallback-only"})

	err := root.Execute()
	require.ErrorContains(t, err, "unknown flag")
}

func TestConfigureDoesNotRequireStateOrGroundControl(t *testing.T) {
	opts := testOptions()
	opts.ConfigDir = t.TempDir()
	opts.NoRegistryFallback = true
	command := newConfigureCommand(opts, nil)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)

	require.NoError(t, command.Execute())
	require.Contains(t, output.String(), "Container runtime configuration complete.")
}

func TestInitializeConfigRequiresAnInputSource(t *testing.T) {
	opts := testOptions()

	_, _, _, err := initializeConfig(opts, true)
	require.ErrorContains(t, err, "--config-dir is required")
}

func TestInitializeConfigFromDirectoryWithoutGroundControl(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, config.Config{StateConfig: standaloneStateConfig()})
	opts := testOptions()
	opts.ConfigDir = dir

	_, cm, _, err := initializeConfig(opts, true)
	require.NoError(t, err)
	require.True(t, cm.IsZTRDone())
	require.False(t, cm.HasGroundControl())
}

func TestInitializeConfigRejectsPartialStateConfig(t *testing.T) {
	dir := t.TempDir()
	state := standaloneStateConfig()
	state.StateURL = ""
	writeConfigFile(t, dir, config.Config{StateConfig: state})
	opts := testOptions()
	opts.ConfigDir = dir

	_, _, _, err := initializeConfig(opts, true)
	require.ErrorContains(t, err, "missing required field(s): state")
}

func TestInitializeConfigFromStateFlags(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	opts := testOptions()
	opts.StateURL = "https://harbor.example.com/satellite/state/example:latest"
	opts.StateAuthURL = "https://harbor.example.com"
	opts.StateAuthUsername = "robot$satellite"
	opts.StateAuthPassword = "secret"

	pathConfig, cm, _, err := initializeConfig(opts, true)
	require.NoError(t, err)
	require.NotEmpty(t, pathConfig.ConfigDir)
	require.Equal(t, standaloneStateConfig(), cm.GetStateConfig())
}

func TestInitializeConfigFromGroundControlBootstrap(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	opts := testOptions()
	opts.GroundControlURL = "https://ground-control.example.com"
	opts.Token = "bootstrap-token"

	_, cm, _, err := initializeConfig(opts, true)
	require.NoError(t, err)
	require.False(t, cm.IsZTRDone())
	require.True(t, cm.HasGroundControl())
}

func TestInitializeConfigFromSPIFFEBootstrap(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	opts := testOptions()
	opts.GroundControlURL = "https://ground-control.example.com"
	opts.SPIFFEEnabled = true

	_, cm, _, err := initializeConfig(opts, true)
	require.NoError(t, err)
	require.True(t, cm.IsSPIFFEEnabled())
}

func TestInitializeConfigFromFileSPIFFEBootstrap(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, config.Config{AppConfig: config.AppConfig{
		GroundControlURL: "https://ground-control.example.com",
		SPIFFE:           config.SPIFFEConfig{Enabled: true},
	}})
	opts := testOptions()
	opts.ConfigDir = dir

	_, cm, _, err := initializeConfig(opts, true)
	require.NoError(t, err)
	require.True(t, cm.IsSPIFFEEnabled())
}
