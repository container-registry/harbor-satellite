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
	"strconv"
	"time"

	_ "github.com/joho/godotenv/autoload"
	_ "github.com/lib/pq"

	"github.com/container-registry/harbor-satellite/ground-control/internal/auth"
	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
	"github.com/container-registry/harbor-satellite/ground-control/internal/middleware"
	"github.com/container-registry/harbor-satellite/ground-control/internal/spiffe"
)

type Server struct {
	port           int
	db             *sql.DB
	dbQueries      *database.Queries
	rateLimiter    *middleware.RateLimiter
	spiffeProvider spiffe.Provider
	embeddedSpire  *spiffe.EmbeddedSpireServer

	// User auth
	passwordPolicy  auth.PasswordPolicy
	sessionDuration time.Duration
	lockoutDuration time.Duration

	// Satellite status
	staleThreshold time.Duration
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
	TLSConfig      *TLSConfig
	CertWatcher    *middleware.CertWatcher
	SPIFFEProvider spiffe.Provider
	SPIFFEConfig   *spiffe.Config
	EmbeddedSpire  *spiffe.EmbeddedSpireServer
}

var (
	dbName   = os.Getenv("DB_DATABASE")
	password = os.Getenv("DB_PASSWORD")
	username = os.Getenv("DB_USERNAME")
	PORT     = os.Getenv("DB_PORT")
	HOST     = os.Getenv("DB_HOST")
)

func NewServer() *ServerResult {
	port, err := strconv.Atoi(os.Getenv("PORT"))
	if err != nil {
		log.Fatalf("PORT is not valid: %v", err)
	}

	connStr := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		username,
		password,
		HOST,
		PORT,
		dbName,
	)

	db, err := sql.Open("postgres", connStr)
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
	if os.Getenv("EMBEDDED_SPIRE_ENABLED") == "true" {
		spireCfg := &spiffe.EmbeddedSpireConfig{
			Enabled:     true,
			DataDir:     getEnvOrDefault("SPIRE_DATA_DIR", "/tmp/spire-data"),
			TrustDomain: getEnvOrDefault("SPIRE_TRUST_DOMAIN", "harbor-satellite.local"),
			BindAddress: getEnvOrDefault("SPIRE_BIND_ADDRESS", "127.0.0.1"),
			BindPort:    8081,
		}
		embeddedSpire = spiffe.NewEmbeddedSpireServer(spireCfg)
		if err := embeddedSpire.Start(context.Background()); err != nil {
			log.Fatalf("Failed to start embedded SPIRE server: %v", err)
		}
	}

	newServer := &Server{
		port:           port,
		db:             db,
		dbQueries:      dbQueries,
		rateLimiter:    rateLimiter,
		spiffeProvider: spiffeProvider,
		embeddedSpire:  embeddedSpire,

		// User auth settings
		passwordPolicy:  auth.LoadPolicyFromEnv(),
		sessionDuration: parseDurationEnv("SESSION_DURATION", 24*time.Hour),
		lockoutDuration: parseDurationEnv("LOCKOUT_DURATION", 5*time.Minute),

		// Satellite status
		staleThreshold: parseDurationEnv("STALE_THRESHOLD", time.Hour),
	}

	tlsCfg := loadTLSConfig()

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", newServer.port),
		Handler:      newServer.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
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

