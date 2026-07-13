package env

import (
	"testing"
	"time"
)

func TestLoadParsesGroundControlEnvironment(t *testing.T) {
	t.Setenv("DB_HOST", "postgres")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USERNAME", "gc")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("DB_DATABASE", "groundcontrol")
	t.Setenv("HARBOR_URL", "https://harbor.example")
	t.Setenv("HARBOR_USERNAME", "robot")
	t.Setenv("HARBOR_PASSWORD", "robot-secret")
	t.Setenv("PORT", "8080")
	t.Setenv("SESSION_DURATION", "12h")
	t.Setenv("SPIFFE_SERVER_ENABLED", "true")
	t.Setenv("SPIFFE_PROVIDER", "static")
	t.Setenv("SPIRE_SERVER_PORT", "9090")
	t.Setenv("EMBEDDED_SPIRE_ENABLED", "true")
	t.Setenv("AUDIT_LOG_ENABLED", "true")
	t.Setenv("AUDIT_SYSLOG_FILE_PATH", "/tmp/audit.log")

	if err := LoadGC(); err != nil {
		t.Fatalf("LoadGC() error = %v", err)
	}
	cfg := GC

	if cfg.Database.Host != "postgres" || cfg.Database.Database != "groundcontrol" {
		t.Fatalf("database env was not parsed: %+v", cfg.Database)
	}
	if cfg.Harbor.URL != "https://harbor.example" || cfg.Harbor.Username != "robot" {
		t.Fatalf("harbor env was not parsed: %+v", cfg.Harbor)
	}
	if cfg.Server.Port != 8080 || cfg.Server.SessionDuration != 12*time.Hour {
		t.Fatalf("server env was not parsed: %+v", cfg.Server)
	}
	if !cfg.SPIFFE.Enabled || cfg.SPIFFE.Provider != "static" {
		t.Fatalf("spiffe env was not parsed: %+v", cfg.SPIFFE)
	}
	if !cfg.EmbeddedSPIRE.Enabled || cfg.SPIRE.ServerPort != 9090 {
		t.Fatalf("spire env was not parsed: embedded=%+v spire=%+v", cfg.EmbeddedSPIRE, cfg.SPIRE)
	}
	if !cfg.Audit.LogEnabled || cfg.Audit.SyslogFilePath == nil || *cfg.Audit.SyslogFilePath != "/tmp/audit.log" {
		t.Fatalf("audit env was not parsed: %+v", cfg.Audit)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	if err := LoadGC(); err != nil {
		t.Fatalf("LoadGC() error = %v", err)
	}
	cfg := GC

	if cfg.PasswordPolicy.MinLength != 8 || !cfg.PasswordPolicy.RequireUppercase {
		t.Fatalf("password policy defaults were not applied: %+v", cfg.PasswordPolicy)
	}
	if cfg.Server.Port != 8080 {
		t.Fatalf("server port default = %d, want 8080", cfg.Server.Port)
	}
	if cfg.SPIFFE.TrustDomain != "harbor-satellite.local" {
		t.Fatalf("SPIFFE trust domain default = %q", cfg.SPIFFE.TrustDomain)
	}
	if cfg.Audit.SyslogTarget != "file" || !cfg.Audit.SyslogFileCompress {
		t.Fatalf("audit defaults were not applied: %+v", cfg.Audit)
	}
	if got := cfg.Harbor.RobotDurationDaysValue(); got != 30 {
		t.Fatalf("RobotDurationDaysValue() = %d, want 30", got)
	}
}

func TestLoadSatelliteParsesEnvironment(t *testing.T) {
	t.Setenv("TOKEN", "token-123")
	t.Setenv("GROUND_CONTROL_URL", "https://gc.example")
	t.Setenv("SPIFFE_ENABLED", "true")
	t.Setenv("USE_UNSECURE", "true")
	t.Setenv("BYO_REGISTRY", "true")
	t.Setenv("REGISTRY_URL", "https://registry.example")
	t.Setenv("REGISTRY_USERNAME", "satellite")
	t.Setenv("REGISTRY_PASSWORD", "secret")
	t.Setenv("CONFIG_DIR", "/tmp/satellite-config")
	t.Setenv("REGISTRY_DATA_DIR", "/tmp/registry")
	t.Setenv("SHUTDOWN_TIMEOUT", "45s")
	t.Setenv("NO_REGISTRY_FALLBACK", "true")
	t.Setenv("HARBOR_REGISTRY_URL", "https://harbor.example")
	t.Setenv("DIRECT_DELIVERY", "true")
	t.Setenv("IMAGE_DIR", "/var/lib/rancher/k3s/agent/images")

	if err := LoadSatellite(); err != nil {
		t.Fatalf("LoadSatellite() error = %v", err)
	}
	cfg := Satellite

	if cfg.Token != "token-123" || cfg.GroundControlURL != "https://gc.example" {
		t.Fatalf("satellite identity env was not parsed: %+v", cfg)
	}
	if !cfg.SPIFFEEnabled || !cfg.UseUnsecure || !cfg.BYORegistry {
		t.Fatalf("satellite boolean env was not parsed: %+v", cfg)
	}
	if cfg.RegistryDataDir != "/tmp/registry" || cfg.ShutdownTimeout != "45s" {
		t.Fatalf("satellite path/timing env was not parsed: %+v", cfg)
	}
}
