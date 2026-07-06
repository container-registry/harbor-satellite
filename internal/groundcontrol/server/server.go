package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/lib/pq"

	"github.com/container-registry/harbor-satellite/internal/env"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/auth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	auditlog "github.com/container-registry/harbor-satellite/internal/groundcontrol/logger"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/middleware"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
)

type Server struct {
	port           int
	db             *sql.DB
	dbQueries      *database.Queries
	rateLimiter    *middleware.RateLimiter
	spiffeProvider spiffe.Provider
	embeddedSpire  *spiffe.EmbeddedSpireServer
	spireClient    *spiffe.ServerClient

	// External SPIRE server metadata (used when embeddedSpire is nil)
	spireServerAddress string
	spireServerPort    int
	spireTrustDomain   string

	// User auth
	passwordPolicy  auth.PasswordPolicy
	sessionDuration time.Duration
	lockoutDuration time.Duration

	// Satellite status
	staleThreshold time.Duration

	// Audit logger for security events
	audit *auditlog.AuditLogger

	// trustForwardedHeaders controls whether clientIP() honors
	// X-Forwarded-For / X-Real-IP. Disabled by default to prevent clients
	// from spoofing the audit source_ip. Enable only when GC sits behind a
	// trusted reverse proxy.
	trustForwardedHeaders bool
}

// TLSConfig holds TLS settings for the server.
type TLSConfig struct {
	CertFile string
	KeyFile  string
	CAFile   string
	Enabled  bool
}

// ServerResult contains the http.Server and TLS configuration.
type ServerResult struct {
	Server         *http.Server
	AppServer      *Server
	TLSConfig      *TLSConfig
	CertWatcher    *middleware.CertWatcher
	SPIFFEProvider spiffe.Provider
	SPIFFEConfig   *spiffe.Config
	EmbeddedSpire  *spiffe.EmbeddedSpireServer
}

func NewServer() *ServerResult {
	cfg := env.GC

	db, err := sql.Open("postgres", cfg.Database.URL())
	if err != nil {
		log.Fatalf("Error in sql: %v", err)
	}

	dbQueries := database.New(db)

	// Initialize rate limiter: 10 requests per minute per IP for ZTR endpoint
	rateLimiter := middleware.NewRateLimiter(10, time.Minute)

	// Load SPIFFE configuration
	spiffeCfg := spiffe.LoadConfig()

	var spiffeProvider spiffe.Provider
	if spiffeCfg.Enabled {
		spiffeProvider, err = spiffe.NewProvider(spiffeCfg)
		if err != nil {
			log.Fatalf("Failed to create SPIFFE provider: %v", err)
		}
		log.Printf("SPIFFE enabled with trust domain: %s", spiffeCfg.TrustDomain)
	}

	// Start embedded SPIRE server if enabled
	var embeddedSpire *spiffe.EmbeddedSpireServer
	if cfg.EmbeddedSPIRE.Enabled {
		spireCfg := &spiffe.EmbeddedSpireConfig{
			Enabled:     true,
			DataDir:     cfg.SPIRE.DataDir,
			TrustDomain: cfg.SPIRE.TrustDomain,
			BindAddress: cfg.SPIRE.BindAddress,
			BindPort:    8081,
		}
		embeddedSpire = spiffe.NewEmbeddedSpireServer(spireCfg)
		if err := embeddedSpire.Start(context.Background()); err != nil {
			log.Fatalf("Failed to start embedded SPIRE server: %v", err)
		}
	}

	// Initialize SPIRE client: prefer embedded, fall back to external socket
	var spireClient *spiffe.ServerClient
	var spireServerAddress string
	var spireServerPort int
	var spireTrustDomain string

	if embeddedSpire != nil {
		spireClient = embeddedSpire.GetClient()
		spireServerAddress = embeddedSpire.GetBindAddress()
		spireServerPort = embeddedSpire.GetBindPort()
		spireTrustDomain = embeddedSpire.GetTrustDomain()
	} else if socketPath := cfg.SPIRE.ServerSocket; socketPath != "" {
		spireTrustDomain = cfg.SPIRE.TrustDomain
		var clientErr error
		spireClient, clientErr = spiffe.NewServerClient(socketPath, spireTrustDomain)
		if clientErr != nil {
			log.Printf("Warning: Failed to connect to external SPIRE server at %q: %v", socketPath, clientErr)
		} else {
			log.Printf("Connected to external SPIRE server at %q (trust domain: %q)", socketPath, spireTrustDomain)
		}
		spireServerAddress = cfg.SPIRE.ServerAddress
		spireServerPort = cfg.SPIRE.ServerPort
	}

	auditCfg, auditErr := cfg.Audit.Config()
	if auditErr != nil {
		log.Fatalf("Failed to load audit config: %v", auditErr)
	}
	auditLogger, auditErr := auditlog.NewAuditLogger(auditCfg, auditlog.ComponentGroundControl)
	if auditErr != nil {
		log.Fatalf("Failed to initialize audit logger: %v", auditErr)
	}

	newServer := &Server{
		port:           cfg.Server.Port,
		db:             db,
		dbQueries:      dbQueries,
		rateLimiter:    rateLimiter,
		spiffeProvider: spiffeProvider,
		embeddedSpire:  embeddedSpire,
		spireClient:    spireClient,

		spireServerAddress: spireServerAddress,
		spireServerPort:    spireServerPort,
		spireTrustDomain:   spireTrustDomain,

		// User auth settings
		passwordPolicy:  auth.LoadPolicyFromConfig(cfg.PasswordPolicy),
		sessionDuration: cfg.Server.SessionDuration,
		lockoutDuration: cfg.Server.LockoutDuration,

		// Satellite status
		staleThreshold: cfg.Server.StaleThreshold,

		// Audit logger
		audit:                 auditLogger,
		trustForwardedHeaders: cfg.Audit.TrustForwardedHeaders,
	}

	// Bootstrap system admin user if not exists
	if err := newServer.BootstrapSystemAdmin(context.Background()); err != nil {
		log.Fatalf("Failed to bootstrap system admin: %v", err)
	}

	tlsCfg := &TLSConfig{
		CertFile: cfg.TLS.CertFile,
		KeyFile:  cfg.TLS.KeyFile,
		CAFile:   cfg.TLS.CAFile,
		Enabled:  cfg.TLS.Enabled(),
	}

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", newServer.port),
		Handler:           newServer.RegisterRoutes(),
		IdleTimeout:       time.Minute,
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	var certWatcher *middleware.CertWatcher

	// Configure TLS: prefer SPIFFE if enabled, fall back to file-based TLS
	if spiffeCfg.Enabled && spiffeProvider != nil {
		tlsConfig, err := buildSPIFFETLSConfig(spiffeProvider, spiffeCfg)
		if err != nil {
			log.Fatalf("Failed to build SPIFFE TLS config: %v", err)
		}
		httpServer.TLSConfig = tlsConfig
		log.Println("Using SPIFFE-based mTLS for server authentication")
	} else if tlsCfg.Enabled {
		// Create certificate watcher for hot-reload
		var err error
		certWatcher, err = middleware.NewCertWatcher(tlsCfg.CertFile, tlsCfg.KeyFile)
		if err != nil {
			log.Fatalf("Failed to create certificate watcher: %v", err)
		}

		tlsConfig, err := buildServerTLSConfigWithWatcher(tlsCfg, certWatcher)
		if err != nil {
			log.Fatalf("Failed to load TLS config: %v", err)
		}
		httpServer.TLSConfig = tlsConfig

		// Start watching for certificate changes (check every 30 seconds)
		certWatcher.Start(30 * time.Second)
		log.Println("Certificate watcher started for TLS hot-reload")
	}

	return &ServerResult{
		Server:         httpServer,
		AppServer:      newServer,
		TLSConfig:      tlsCfg,
		CertWatcher:    certWatcher,
		SPIFFEProvider: spiffeProvider,
		SPIFFEConfig:   spiffeCfg,
		EmbeddedSpire:  embeddedSpire,
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func parseIntEnv(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultValue
}

// parseRequiredIntEnv returns the int value of an env var, or defaultValue if
// the var is unset. If the var is set but cannot be parsed as an integer, the
// process exits. Use for env values where silent fallback to a default would
// mask operator misconfiguration (e.g. audit log rotation knobs).
func parseRequiredIntEnv(key string, defaultValue int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Fatalf("%q must be an integer, got %q", key, v) //nolint:gosec // Logs for diagnostics purpose.
	}
	return n
}

func parseDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultValue
}

func loadTLSConfig() *TLSConfig {
	certFile := os.Getenv("TLS_CERT_FILE")
	keyFile := os.Getenv("TLS_KEY_FILE")
	caFile := os.Getenv("TLS_CA_FILE")

	enabled := certFile != "" && keyFile != ""

	return &TLSConfig{
		CertFile: certFile,
		KeyFile:  keyFile,
		CAFile:   caFile,
		Enabled:  enabled,
	}
}

// buildServerTLSConfigWithWatcher creates a TLS config that uses the certificate watcher
// for dynamic certificate reloading.
func buildServerTLSConfigWithWatcher(cfg *TLSConfig, cw *middleware.CertWatcher) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: cw.GetCertificate,
	}

	if cfg.CAFile != "" {
		caData, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}

		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("invalid CA certificate")
		}

		tlsConfig.ClientCAs = caPool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return tlsConfig, nil
}

// loadAuditConfig reads audit log settings from environment variables.
// Disabled by default; set AUDIT_LOG_ENABLED=true to turn on. The syslog target
// (daemon | network | file) selects the destination; only the relevant fields
// are read. Invalid input exits the process rather than silently propagating to
// the logger config.
func loadAuditConfig() auditlog.AuditConfig {
	if os.Getenv("AUDIT_LOG_ENABLED") != "true" {
		return auditlog.AuditConfig{}
	}

	syslog := loadSyslogConfig()
	otel := loadOtelConfig()
	if !syslog.Enabled && !otel.Enabled {
		log.Fatalf("AUDIT_LOG_ENABLED=true but no transport is enabled: set AUDIT_SYSLOG_ENABLED=true and/or AUDIT_OTEL_ENDPOINT")
	}
	return auditlog.AuditConfig{Enabled: true, Syslog: syslog, OTel: otel}
}

// loadSyslogConfig reads the syslog transport settings. It is on by default
// (AUDIT_SYSLOG_ENABLED=true); set AUDIT_SYSLOG_ENABLED=false to run, for
// example, only the otel transport. The target selects the sink and only the
// relevant fields are read.
func loadSyslogConfig() auditlog.SyslogConfig {
	if !parseBoolEnv("AUDIT_SYSLOG_ENABLED", true) {
		return auditlog.SyslogConfig{Enabled: false}
	}

	target := getEnvOrDefault("AUDIT_SYSLOG_TARGET", "file")
	syslog := auditlog.SyslogConfig{
		Enabled:    true,
		Target:     auditlog.SyslogTarget(target),
		Tag:        getEnvOrDefault("AUDIT_SYSLOG_TAG", "harbor-audit"),
		SocketPath: getEnvOrDefault("AUDIT_SYSLOG_SOCKET_PATH", "/dev/log"),
		Network:    getEnvOrDefault("AUDIT_SYSLOG_NETWORK", "udp"),
		Address:    os.Getenv("AUDIT_SYSLOG_ADDRESS"),
	}

	switch target {
	case "file":
		syslog.File = loadSyslogFileConfig()
	case "network":
		if syslog.Address == "" {
			log.Fatalf("AUDIT_SYSLOG_TARGET=network but AUDIT_SYSLOG_ADDRESS is empty")
		}
	case "daemon":
		// SocketPath already defaulted above.
	default:
		log.Fatalf("AUDIT_SYSLOG_TARGET must be one of daemon|network|file, got %q", target)
	}

	return syslog
}

// parseBoolEnv reads a boolean env var, returning def when unset and exiting on
// an unparseable value (so a typo fails loudly rather than silently flipping).
func parseBoolEnv(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		log.Fatalf("%s must be a boolean (true/false), got %q", key, v)
	}
	return b
}

// loadOtelConfig reads the optional OTLP/HTTP export settings. The transport
// is enabled by setting AUDIT_OTEL_ENDPOINT to the collector base URL (e.g.
// http://127.0.0.1:4318); the standard /v1/logs path is appended when the URL
// carries no path of its own.
func loadOtelConfig() auditlog.OTelConfig {
	endpoint := os.Getenv("AUDIT_OTEL_ENDPOINT")
	return auditlog.OTelConfig{Enabled: endpoint != "", Endpoint: endpoint}
}

// loadSyslogFileConfig reads the file-target rotation settings, validating them
// the same way the satellite config does (invalid values exit the process).
func loadSyslogFileConfig() auditlog.SyslogFileConfig {
	// Distinguish unset (fall back to default) from set-but-empty (operator typo
	// — fail loudly).
	path, isSet := os.LookupEnv("AUDIT_SYSLOG_FILE_PATH")
	if !isSet {
		path = "./audit.log"
	} else if path == "" {
		log.Fatalf("AUDIT_SYSLOG_TARGET=file but AUDIT_SYSLOG_FILE_PATH is empty")
	}

	maxSizeMB := parseRequiredIntEnv("AUDIT_SYSLOG_FILE_MAX_SIZE_MB", 100)
	maxBackups := parseRequiredIntEnv("AUDIT_SYSLOG_FILE_MAX_BACKUPS", 7)
	maxAgeDays := parseRequiredIntEnv("AUDIT_SYSLOG_FILE_MAX_AGE_DAYS", 30)

	if maxSizeMB < 1 {
		log.Fatalf("AUDIT_SYSLOG_FILE_MAX_SIZE_MB must be >= 1, got %d", maxSizeMB)
	}
	if maxBackups < 0 {
		log.Fatalf("AUDIT_SYSLOG_FILE_MAX_BACKUPS must be >= 0, got %d", maxBackups)
	}
	if maxAgeDays < 0 {
		log.Fatalf("AUDIT_SYSLOG_FILE_MAX_AGE_DAYS must be >= 0, got %d", maxAgeDays)
	}

	// Compression defaults on; only an explicit "false" disables it.
	compress := os.Getenv("AUDIT_SYSLOG_FILE_COMPRESS") != "false"

	return auditlog.SyslogFileConfig{
		Path:       path,
		MaxSizeMB:  maxSizeMB,
		MaxBackups: maxBackups,
		MaxAgeDays: maxAgeDays,
		Compress:   compress,
	}
}
