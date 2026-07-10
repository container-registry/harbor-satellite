package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type validateTestCase struct {
	name           string
	config         *Config
	expectError    bool
	expectedErrMsg string
	expectWarnings bool
	expectedConfig *Config
}

func TestValidateAndEnforceDefaults(t *testing.T) {
	expectedZotConfigJSON := DefaultZotConfigJSON

	tests := []validateTestCase{
		{
			name: "valid config",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL:          URL("https://example.com"),
					LogLevel:                  "info",
					StateReplicationInterval:  "0 * * * *",
					RegisterSatelliteInterval: "*/5 * * * *",
				},
				ZotConfigRaw: []byte(`{"distSpecVersion":"1.1.0"}`),
			},
			expectError:    false,
			expectWarnings: true,
		},
		{
			name:           "nil config",
			config:         nil,
			expectError:    false,
			expectedErrMsg: "nil config",
			expectWarnings: true,
		},
		{
			name:           "empty config - defaults applied",
			config:         &Config{},
			expectError:    false,
			expectWarnings: true,
			expectedConfig: &Config{
				AppConfig: AppConfig{
					GroundControlURL:          URL(DefaultGroundControlURL),
					LogLevel:                  zerolog.LevelInfoValue,
					StateReplicationInterval:  DefaultFetchAndReplicateCronExpr,
					RegisterSatelliteInterval: DefaultZTRCronExpr,
				},
				ZotConfigRaw: []byte(expectedZotConfigJSON),
			},
		},
		{
			name: "invalid ground control URL",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL: URL("ht@tp://bad-url"),
				},
				ZotConfigRaw: []byte(DefaultZotConfigJSON),
			},
			expectError:    false,
			expectWarnings: true,
		},
		{
			name: "empty zot config - fallback to default",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL: URL("https://example.com"),
				},
				ZotConfigRaw: []byte(""),
			},
			expectError:    false,
			expectWarnings: true,
			expectedConfig: &Config{
				ZotConfigRaw: []byte(expectedZotConfigJSON),
			},
		},
		{
			name: "invalid log level",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL: URL("https://example.com"),
					LogLevel:         "badlevel",
				},
				ZotConfigRaw: []byte(DefaultZotConfigJSON),
			},
			expectError:    false,
			expectWarnings: true,
			expectedConfig: &Config{
				AppConfig: AppConfig{
					LogLevel: zerolog.LevelInfoValue,
				},
			},
		},
		{
			name: "invalid cron expressions",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL:          URL("https://example.com"),
					StateReplicationInterval:  "bad cron",
					RegisterSatelliteInterval: "also bad",
				},
				ZotConfigRaw: []byte(DefaultZotConfigJSON),
			},
			expectError:    false,
			expectWarnings: true,
			expectedConfig: &Config{
				AppConfig: AppConfig{
					StateReplicationInterval:  DefaultFetchAndReplicateCronExpr,
					RegisterSatelliteInterval: DefaultZTRCronExpr,
				},
			},
		},
		{
			name: "bring own registry missing URL",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL: URL("https://example.com"),
					BringOwnRegistry: true,
					LocalRegistryCredentials: RegistryCredentials{
						Username: "user",
						Password: "pass",
					},
				},
			},
			expectError:    true,
			expectedErrMsg: "custom registry URL is required",
		},
		{
			name: "bring own registry with invalid URL",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL: URL("https://example.com"),
					BringOwnRegistry: true,
					LocalRegistryCredentials: RegistryCredentials{
						URL:      URL("not-a-valid-url"),
						Username: "user",
						Password: "pass",
					},
				},
			},
			expectError:    true,
			expectedErrMsg: "invalid custom registry URL",
		},
		{
			name: "bring own registry with valid config",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL: URL("https://example.com"),
					BringOwnRegistry: true,
					LocalRegistryCredentials: RegistryCredentials{
						URL:      URL("https://myregistry.example.com"),
						Username: "user",
						Password: "pass",
					},
				},
			},
			expectError:    false,
			expectWarnings: true,
		},
		{
			name: "bring own registry missing credentials warns",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL: URL("https://example.com"),
					BringOwnRegistry: true,
					LocalRegistryCredentials: RegistryCredentials{
						URL: URL("https://myregistry.example.com"),
					},
				},
			},
			expectError:    false,
			expectWarnings: true,
		},
		{
			name: "bring own registry with redundant zot config warns",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL: URL("https://example.com"),
					BringOwnRegistry: true,
					LocalRegistryCredentials: RegistryCredentials{
						URL:      URL("https://myregistry.example.com"),
						Username: "user",
						Password: "pass",
					},
				},
				ZotConfigRaw: []byte(`{"distSpecVersion":"1.1.0"}`),
			},
			expectError:    false,
			expectWarnings: true,
		},
		{
			name: "invalid heartbeat interval defaults",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL:  URL("https://example.com"),
					HeartbeatInterval: "invalid cron",
				},
				ZotConfigRaw: []byte(DefaultZotConfigJSON),
			},
			expectError:    false,
			expectWarnings: true,
			expectedConfig: &Config{
				AppConfig: AppConfig{
					HeartbeatInterval: DefaultHeartbeatCronExpr,
				},
			},
		},
		{
			name: "empty cron expression defaults",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL:          URL("https://example.com"),
					StateReplicationInterval:  "",
					RegisterSatelliteInterval: "",
					HeartbeatInterval:         "",
				},
				ZotConfigRaw: []byte(DefaultZotConfigJSON),
			},
			expectError:    false,
			expectWarnings: true,
			expectedConfig: &Config{
				AppConfig: AppConfig{
					StateReplicationInterval:  DefaultFetchAndReplicateCronExpr,
					RegisterSatelliteInterval: DefaultZTRCronExpr,
					HeartbeatInterval:         DefaultHeartbeatCronExpr,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, warnings, err := ValidateAndEnforceDefaults(tt.config, DefaultGroundControlURL)

			if tt.expectError {
				require.Error(t, err)
				if tt.expectedErrMsg != "" {
					require.Contains(t, err.Error(), tt.expectedErrMsg)
				}
				return
			}

			require.NoError(t, err)

			if tt.expectWarnings {
				require.NotEmpty(t, warnings)
			} else {
				require.Empty(t, warnings)
			}

			if tt.expectedConfig != nil {
				exp := tt.expectedConfig.AppConfig
				got := config.AppConfig
				if exp.LogLevel != "" {
					require.Equal(t, exp.LogLevel, got.LogLevel)
				}
				if exp.StateReplicationInterval != "" {
					require.Equal(t, exp.StateReplicationInterval, got.StateReplicationInterval)
				}
				if exp.RegisterSatelliteInterval != "" {
					require.Equal(t, exp.RegisterSatelliteInterval, got.RegisterSatelliteInterval)
				}
				if exp.HeartbeatInterval != "" {
					require.Equal(t, exp.HeartbeatInterval, got.HeartbeatInterval)
				}
				if len(tt.expectedConfig.ZotConfigRaw) > 0 {
					require.JSONEq(t, string(tt.expectedConfig.ZotConfigRaw), string(config.ZotConfigRaw))
				}
			}
		})
	}
}

func TestValidateTLSConfig(t *testing.T) {
	t.Run("valid TLS with cert and key files", func(t *testing.T) {
		tmpDir := t.TempDir()
		certFile := filepath.Join(tmpDir, "cert.pem")
		keyFile := filepath.Join(tmpDir, "key.pem")

		require.NoError(t, os.WriteFile(certFile, []byte("cert"), 0o600))
		require.NoError(t, os.WriteFile(keyFile, []byte("key"), 0o600))

		config := &Config{
			AppConfig: AppConfig{
				GroundControlURL: URL("https://example.com"),
				TLS: TLSConfig{
					CertFile: certFile,
					KeyFile:  keyFile,
				},
			},
			ZotConfigRaw: []byte(DefaultZotConfigJSON),
		}

		_, warnings, err := ValidateAndEnforceDefaults(config, DefaultGroundControlURL)
		require.NoError(t, err)
		require.NotContains(t, warnings, "cert_file")
	})

	t.Run("error when only cert_file provided", func(t *testing.T) {
		tmpDir := t.TempDir()
		certFile := filepath.Join(tmpDir, "cert.pem")
		require.NoError(t, os.WriteFile(certFile, []byte("cert"), 0o600))

		config := &Config{
			AppConfig: AppConfig{
				GroundControlURL: URL("https://example.com"),
				TLS: TLSConfig{
					CertFile: certFile,
				},
			},
			ZotConfigRaw: []byte(DefaultZotConfigJSON),
		}

		_, _, err := ValidateAndEnforceDefaults(config, DefaultGroundControlURL)
		require.Error(t, err)
		require.Contains(t, err.Error(), "both cert_file and key_file must be provided")
	})

	t.Run("error when only key_file provided", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyFile := filepath.Join(tmpDir, "key.pem")
		require.NoError(t, os.WriteFile(keyFile, []byte("key"), 0o600))

		config := &Config{
			AppConfig: AppConfig{
				GroundControlURL: URL("https://example.com"),
				TLS: TLSConfig{
					KeyFile: keyFile,
				},
			},
			ZotConfigRaw: []byte(DefaultZotConfigJSON),
		}

		_, _, err := ValidateAndEnforceDefaults(config, DefaultGroundControlURL)
		require.Error(t, err)
		require.Contains(t, err.Error(), "both cert_file and key_file must be provided")
	})

	t.Run("error when cert_file does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyFile := filepath.Join(tmpDir, "key.pem")
		require.NoError(t, os.WriteFile(keyFile, []byte("key"), 0o600))

		config := &Config{
			AppConfig: AppConfig{
				GroundControlURL: URL("https://example.com"),
				TLS: TLSConfig{
					CertFile: "/nonexistent/cert.pem",
					KeyFile:  keyFile,
				},
			},
			ZotConfigRaw: []byte(DefaultZotConfigJSON),
		}

		_, _, err := ValidateAndEnforceDefaults(config, DefaultGroundControlURL)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cert_file not found")
	})

	t.Run("warning when skip_verify enabled with ca_file", func(t *testing.T) {
		tmpDir := t.TempDir()
		caFile := filepath.Join(tmpDir, "ca.pem")
		require.NoError(t, os.WriteFile(caFile, []byte("ca"), 0o600))

		config := &Config{
			AppConfig: AppConfig{
				GroundControlURL: URL("https://example.com"),
				TLS: TLSConfig{
					CAFile:     caFile,
					SkipVerify: true,
				},
			},
			ZotConfigRaw: []byte(DefaultZotConfigJSON),
		}

		_, warnings, err := ValidateAndEnforceDefaults(config, DefaultGroundControlURL)
		require.NoError(t, err)
		found := false
		for _, w := range warnings {
			if w == "TLS skip_verify is enabled, certificate verification will be skipped" {
				found = true
				break
			}
		}
		require.True(t, found, "expected skip_verify warning")
	})
}

func TestValidateRegistryFallbackConfig(t *testing.T) {
	baseConfig := func() *Config {
		return &Config{
			AppConfig: AppConfig{
				GroundControlURL: URL("https://example.com"),
			},
			ZotConfigRaw: []byte(DefaultZotConfigJSON),
		}
	}

	t.Run("disabled produces no warnings", func(t *testing.T) {
		cfg := baseConfig()
		cfg.AppConfig.RegistryFallback = RegistryFallbackConfig{Enabled: false}
		_, warnings, err := ValidateAndEnforceDefaults(cfg, DefaultGroundControlURL)
		require.NoError(t, err)
		for _, w := range warnings {
			require.NotContains(t, w, "registry_fallback")
		}
	})

	t.Run("enabled with no registries defaults to docker.io", func(t *testing.T) {
		cfg := baseConfig()
		cfg.AppConfig.RegistryFallback = RegistryFallbackConfig{Enabled: true}
		result, warnings, err := ValidateAndEnforceDefaults(cfg, DefaultGroundControlURL)
		require.NoError(t, err)
		found := false
		for _, w := range warnings {
			if w == "registry_fallback is enabled but no registries specified, defaulting to docker.io" {
				found = true
			}
		}
		require.True(t, found, "expected default registries warning, got: %v", warnings)
		require.Equal(t, []string{"docker.io"}, result.AppConfig.RegistryFallback.Registries)
	})

	t.Run("enabled with valid registries no warning", func(t *testing.T) {
		cfg := baseConfig()
		cfg.AppConfig.RegistryFallback = RegistryFallbackConfig{
			Enabled:    true,
			Registries: []string{"docker.io", "quay.io"},
		}
		_, warnings, err := ValidateAndEnforceDefaults(cfg, DefaultGroundControlURL)
		require.NoError(t, err)
		for _, w := range warnings {
			require.NotContains(t, w, "no registries specified")
		}
	})

	t.Run("enabled with empty registry entry warns", func(t *testing.T) {
		cfg := baseConfig()
		cfg.AppConfig.RegistryFallback = RegistryFallbackConfig{
			Enabled:    true,
			Registries: []string{"docker.io", "  "},
		}
		_, warnings, err := ValidateAndEnforceDefaults(cfg, DefaultGroundControlURL)
		require.NoError(t, err)
		found := false
		for _, w := range warnings {
			if w == "registry_fallback contains an empty registry entry" {
				found = true
			}
		}
		require.True(t, found, "expected empty registry warning")
	})

	t.Run("enabled with unknown runtime warns", func(t *testing.T) {
		cfg := baseConfig()
		cfg.AppConfig.RegistryFallback = RegistryFallbackConfig{
			Enabled:    true,
			Registries: []string{"docker.io"},
			Runtimes:   []string{"docker", "badruntime"},
		}
		_, warnings, err := ValidateAndEnforceDefaults(cfg, DefaultGroundControlURL)
		require.NoError(t, err)
		found := false
		for _, w := range warnings {
			if w == `registry_fallback contains unknown runtime "badruntime", valid values: docker, containerd, crio, podman` {
				found = true
			}
		}
		require.True(t, found, "expected unknown runtime warning, got: %v", warnings)
	})

	t.Run("enabled with all valid runtimes no runtime warning", func(t *testing.T) {
		cfg := baseConfig()
		cfg.AppConfig.RegistryFallback = RegistryFallbackConfig{
			Enabled:    true,
			Registries: []string{"docker.io"},
			Runtimes:   []string{"docker", "containerd", "crio", "podman"},
		}
		_, warnings, err := ValidateAndEnforceDefaults(cfg, DefaultGroundControlURL)
		require.NoError(t, err)
		for _, w := range warnings {
			require.NotContains(t, w, "unknown runtime")
		}
	})
}

func TestUseUnsecureEnvVar(t *testing.T) {
	baseConfig := func() *Config {
		return &Config{
			AppConfig: AppConfig{
				GroundControlURL: URL("https://example.com"),
			},
			ZotConfigRaw: []byte(DefaultZotConfigJSON),
		}
	}

	t.Run("USE_UNSECURE=true sets UseUnsecure", func(t *testing.T) {
		t.Setenv("USE_UNSECURE", "true")
		config := baseConfig()
		result, _, err := ValidateAndEnforceDefaults(config, DefaultGroundControlURL)
		require.NoError(t, err)
		require.True(t, result.AppConfig.UseUnsecure)
	})

	t.Run("USE_UNSECURE=1 sets UseUnsecure", func(t *testing.T) {
		t.Setenv("USE_UNSECURE", "1")
		config := baseConfig()
		result, _, err := ValidateAndEnforceDefaults(config, DefaultGroundControlURL)
		require.NoError(t, err)
		require.True(t, result.AppConfig.UseUnsecure)
	})

	t.Run("USE_UNSECURE=false does not set UseUnsecure", func(t *testing.T) {
		t.Setenv("USE_UNSECURE", "false")
		config := baseConfig()
		result, _, err := ValidateAndEnforceDefaults(config, DefaultGroundControlURL)
		require.NoError(t, err)
		require.False(t, result.AppConfig.UseUnsecure)
	})

	t.Run("USE_UNSECURE empty does not change UseUnsecure", func(t *testing.T) {
		t.Setenv("USE_UNSECURE", "")
		config := baseConfig()
		config.AppConfig.UseUnsecure = false
		result, _, err := ValidateAndEnforceDefaults(config, DefaultGroundControlURL)
		require.NoError(t, err)
		require.False(t, result.AppConfig.UseUnsecure)
	})
}

func TestValidateAndEnforceAuditConfig(t *testing.T) {
	t.Run("disabled leaves zero values untouched", func(t *testing.T) {
		cfg := &Config{}
		warnings := validateAndEnforceAuditConfig(cfg)
		require.Empty(t, warnings)
		require.Equal(t, AuditConfig{}, cfg.AppConfig.Audit)
	})

	t.Run("file target: omitted rotation fields default silently", func(t *testing.T) {
		cfg := &Config{
			AppConfig: AppConfig{
				Audit: AuditConfig{Enabled: true},
			},
		}
		warnings := validateAndEnforceAuditConfig(cfg)
		// Target defaults to file, so only the empty file path warns. Unset
		// size/backups/age default silently — an omitted field is "use the
		// default", not "retain all", matching Ground Control's env-var defaults.
		require.Len(t, warnings, 1)
		f := cfg.AppConfig.Audit.Syslog.File
		require.Equal(t, "file", cfg.AppConfig.Audit.Syslog.Target)
		require.Equal(t, DefaultAuditSyslogTag, cfg.AppConfig.Audit.Syslog.Tag)
		require.Equal(t, DefaultAuditFilePath, f.Path)
		require.Equal(t, DefaultAuditMaxSizeMB, *f.MaxSizeMB)
		require.Equal(t, DefaultAuditMaxBackups, *f.MaxBackups)
		require.Equal(t, DefaultAuditMaxAgeDays, *f.MaxAgeDays)
	})

	t.Run("file target: user-provided values are preserved", func(t *testing.T) {
		cfg := &Config{
			AppConfig: AppConfig{
				Audit: AuditConfig{
					Enabled: true,
					Syslog: SyslogAudit{
						Target: "file",
						File: SyslogAuditFile{
							Path:       "/var/log/audit.log",
							MaxSizeMB:  intPtr(50),
							MaxBackups: intPtr(10),
							MaxAgeDays: intPtr(90),
						},
					},
				},
			},
		}
		warnings := validateAndEnforceAuditConfig(cfg)
		require.Empty(t, warnings)
		f := cfg.AppConfig.Audit.Syslog.File
		require.Equal(t, "/var/log/audit.log", f.Path)
		require.Equal(t, 50, *f.MaxSizeMB)
		require.Equal(t, 10, *f.MaxBackups)
		require.Equal(t, 90, *f.MaxAgeDays)
	})

	t.Run("file target: negative rotation values are corrected with warnings", func(t *testing.T) {
		cfg := &Config{
			AppConfig: AppConfig{
				Audit: AuditConfig{
					Enabled: true,
					Syslog: SyslogAudit{
						Target: "file",
						File: SyslogAuditFile{
							Path:       "./a.log",
							MaxSizeMB:  intPtr(-1),
							MaxBackups: intPtr(-5),
							MaxAgeDays: intPtr(-1),
						},
					},
				},
			},
		}
		warnings := validateAndEnforceAuditConfig(cfg)
		require.Len(t, warnings, 3)
		f := cfg.AppConfig.Audit.Syslog.File
		require.Equal(t, DefaultAuditMaxSizeMB, *f.MaxSizeMB)
		require.Equal(t, DefaultAuditMaxBackups, *f.MaxBackups)
		require.Equal(t, DefaultAuditMaxAgeDays, *f.MaxAgeDays)
	})

	t.Run("file target: explicit zero backups and age are preserved as unlimited retention", func(t *testing.T) {
		cfg := &Config{
			AppConfig: AppConfig{
				Audit: AuditConfig{
					Enabled: true,
					Syslog: SyslogAudit{
						Target: "file",
						File: SyslogAuditFile{
							Path:       "./a.log",
							MaxSizeMB:  intPtr(10),
							MaxBackups: intPtr(0),
							MaxAgeDays: intPtr(0),
						},
					},
				},
			},
		}
		warnings := validateAndEnforceAuditConfig(cfg)
		require.Empty(t, warnings)
		f := cfg.AppConfig.Audit.Syslog.File
		require.Equal(t, 10, *f.MaxSizeMB)
		require.Equal(t, 0, *f.MaxBackups)
		require.Equal(t, 0, *f.MaxAgeDays)
	})

	t.Run("daemon target: socket path defaults, no rotation applied", func(t *testing.T) {
		cfg := &Config{
			AppConfig: AppConfig{
				Audit: AuditConfig{Enabled: true, Syslog: SyslogAudit{Target: "daemon"}},
			},
		}
		warnings := validateAndEnforceAuditConfig(cfg)
		require.Empty(t, warnings)
		require.Equal(t, DefaultAuditSyslogSocket, cfg.AppConfig.Audit.Syslog.SocketPath)
		require.Nil(t, cfg.AppConfig.Audit.Syslog.File.MaxSizeMB, "rotation not applied to daemon target")
	})

	t.Run("network target: missing address warns", func(t *testing.T) {
		cfg := &Config{
			AppConfig: AppConfig{
				Audit: AuditConfig{Enabled: true, Syslog: SyslogAudit{Target: "network", Network: "tcp"}},
			},
		}
		warnings := validateAndEnforceAuditConfig(cfg)
		require.Len(t, warnings, 1, "missing address should warn")
	})

	t.Run("invalid target falls back to file with a warning", func(t *testing.T) {
		cfg := &Config{
			AppConfig: AppConfig{
				Audit: AuditConfig{Enabled: true, Syslog: SyslogAudit{Target: "bogus"}},
			},
		}
		warnings := validateAndEnforceAuditConfig(cfg)
		require.NotEmpty(t, warnings)
		require.Equal(t, "file", cfg.AppConfig.Audit.Syslog.Target)
	})

	t.Run("otel only: disabled syslog is not enforced and does not warn", func(t *testing.T) {
		cfg := &Config{
			AppConfig: AppConfig{
				Audit: AuditConfig{
					Enabled: true,
					Syslog:  SyslogAudit{Enabled: boolPtr(false)},
					Otel:    OtelAudit{Enabled: true, Endpoint: "http://collector:4318"},
				},
			},
		}
		warnings := validateAndEnforceAuditConfig(cfg)
		require.Empty(t, warnings)
		// syslog is off, so its target is left untouched (not defaulted to file).
		require.Empty(t, cfg.AppConfig.Audit.Syslog.Target)
	})

	t.Run("no transport enabled warns", func(t *testing.T) {
		cfg := &Config{
			AppConfig: AppConfig{
				Audit: AuditConfig{Enabled: true, Syslog: SyslogAudit{Enabled: boolPtr(false)}},
			},
		}
		warnings := validateAndEnforceAuditConfig(cfg)
		require.NotEmpty(t, warnings)
	})
}

func TestAuditConfig_Equal(t *testing.T) {
	fileCfg := func() AuditConfig {
		return AuditConfig{
			Enabled: true,
			Syslog:  SyslogAudit{Target: "file", File: SyslogAuditFile{Path: "/a.log"}},
		}
	}

	t.Run("two empty configs are equal", func(t *testing.T) {
		left := AuditConfig{}
		right := AuditConfig{}
		require.True(t, left.Equal(right))
	})

	t.Run("omitted field equals an explicit default value", func(t *testing.T) {
		// nil resolves to the default via the OrDefault helpers, so a reload that
		// only makes the default explicit must NOT count as a change.
		omitted := fileCfg()
		explicit := fileCfg()
		explicit.Syslog.File.MaxSizeMB = intPtr(DefaultAuditMaxSizeMB)
		explicit.Syslog.File.MaxBackups = intPtr(DefaultAuditMaxBackups)
		explicit.Syslog.File.MaxAgeDays = intPtr(DefaultAuditMaxAgeDays)
		explicit.Syslog.File.Compress = boolPtr(DefaultAuditCompress)
		require.True(t, omitted.Equal(explicit))
	})

	t.Run("explicit zero (keep-all) differs from omitted default", func(t *testing.T) {
		// This is the whole point of the pointer fields: 0 means "retain
		// everything" and must be distinguishable from "use the default".
		keepAll := fileCfg()
		keepAll.Syslog.File.MaxBackups = intPtr(0)
		require.False(t, keepAll.Equal(fileCfg()))
	})

	t.Run("a changed file path is detected", func(t *testing.T) {
		other := fileCfg()
		other.Syslog.File.Path = "/b.log"
		require.False(t, fileCfg().Equal(other))
	})

	t.Run("a changed target is detected", func(t *testing.T) {
		other := fileCfg()
		other.Syslog.Target = "daemon"
		require.False(t, fileCfg().Equal(other))
	})

	t.Run("a flipped enabled flag is detected", func(t *testing.T) {
		require.False(t, AuditConfig{Enabled: true}.Equal(AuditConfig{Enabled: false}))
	})

	t.Run("a disabled syslog transport is detected", func(t *testing.T) {
		other := fileCfg()
		other.Syslog.Enabled = boolPtr(false)
		require.False(t, fileCfg().Equal(other))
	})

	t.Run("omitted syslog.enabled equals explicit true", func(t *testing.T) {
		explicit := fileCfg()
		explicit.Syslog.Enabled = boolPtr(true)
		require.True(t, fileCfg().Equal(explicit))
	})
}

func intPtr(i int) *int    { return &i }
func boolPtr(b bool) *bool { return &b }
