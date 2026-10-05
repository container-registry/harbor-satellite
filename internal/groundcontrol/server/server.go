package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/lib/pq"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/auth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/middleware"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	auditlog "github.com/container-registry/harbor-satellite/internal/shared/logger"
)

type Server struct {
	port           int
	db             *sql.DB
	dbQueries      *database.Queries
	rateLimiter    *middleware.RateLimiter
	spiffeProvider spiffe.Provider
	embeddedSpire  *spiffe.EmbeddedSpireServer
	spireClient    *spiffe.ServerClient
	spireEnabled   bool

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

// ServerTLSConfig holds TLS settings for the Ground Control HTTP server.
type ServerTLSConfig struct {
	CertFile string
	KeyFile  string
	CAFile   string
	Enabled  bool
}

// ServerResult contains the http.Server and TLS configuration.
type ServerResult struct {
	Server         *http.Server
	AppServer      *Server
	TLSConfig      *ServerTLSConfig
	CertWatcher    *middleware.CertWatcher
	SPIFFEProvider spiffe.Provider
	SPIFFEConfig   *spiffe.Config
	EmbeddedSpire  *spiffe.EmbeddedSpireServer
}

func NewServer(ctx context.Context) (result *ServerResult, err error) {
	result = &ServerResult{AppServer: &Server{}}
	defer func() {
		if err != nil {
			err = errors.Join(err, result.Close())
			result = nil
		}
	}()
	cfg := env.GC

	db, err := sql.Open("postgres", cfg.Database.URL())
	if err != nil {
		return result, fmt.Errorf("open database: %w", err)
	}
	result.AppServer.db = db

	dbQueries := database.New(db)

	// Initialize rate limiter: 10 requests per minute per IP for ZTR endpoint
	rateLimiter := middleware.NewRateLimiter(10, time.Minute)
	result.AppServer.rateLimiter = rateLimiter

	// Load SPIFFE configuration
	spiffeCfg := spiffe.LoadConfig()

	var spiffeProvider spiffe.Provider
	if spiffeCfg.Enabled {
		spiffeProvider, err = spiffe.NewProvider(spiffeCfg)
		if err != nil {
			return result, fmt.Errorf("create SPIFFE provider: %w", err)
		}
		result.SPIFFEProvider = spiffeProvider
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
		if err := embeddedSpire.Start(ctx); err != nil {
			return result, fmt.Errorf("start embedded SPIRE server: %w", err)
		}
		result.EmbeddedSpire = embeddedSpire
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
	result.AppServer.spireClient = spireClient

	auditCfg, auditErr := cfg.Audit.Config()
	if auditErr != nil {
		return result, fmt.Errorf("load audit config: %w", auditErr)
	}
	auditLogger, auditErr := auditlog.NewAuditLogger(auditCfg, auditlog.ComponentGroundControl)
	if auditErr != nil {
		return result, fmt.Errorf("initialize audit logger: %w", auditErr)
	}

	newServer := &Server{
		port:           cfg.Server.Port,
		db:             db,
		dbQueries:      dbQueries,
		rateLimiter:    rateLimiter,
		spiffeProvider: spiffeProvider,
		embeddedSpire:  embeddedSpire,
		spireClient:    spireClient,
		spireEnabled:   spiffeCfg.Enabled || cfg.EmbeddedSPIRE.Enabled || cfg.SPIRE.ServerSocket != "",

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
	result.AppServer = newServer

	// Bootstrap system admin user if not exists
	if err := newServer.BootstrapSystemAdmin(ctx); err != nil {
		return result, fmt.Errorf("bootstrap system admin: %w", err)
	}

	tlsCfg := &ServerTLSConfig{
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
			return result, fmt.Errorf("build SPIFFE TLS config: %w", err)
		}
		httpServer.TLSConfig = tlsConfig
		log.Println("Using SPIFFE-based mTLS for server authentication")
	} else if tlsCfg.Enabled {
		// Create certificate watcher for hot-reload
		var err error
		certWatcher, err = middleware.NewCertWatcher(tlsCfg.CertFile, tlsCfg.KeyFile)
		if err != nil {
			return result, fmt.Errorf("create certificate watcher: %w", err)
		}
		result.CertWatcher = certWatcher

		tlsConfig, err := buildServerTLSConfigWithWatcher(tlsCfg, certWatcher)
		if err != nil {
			return result, fmt.Errorf("load TLS config: %w", err)
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
	}, nil
}

// Close releases server resources after requests and cleanup work have stopped.
func (r *ServerResult) Close() error {
	var err error
	if r.CertWatcher != nil {
		r.CertWatcher.Stop()
	}
	if r.SPIFFEProvider != nil {
		err = errors.Join(err, r.SPIFFEProvider.Close())
	}
	if r.EmbeddedSpire != nil {
		err = errors.Join(err, r.EmbeddedSpire.Stop())
	} else if r.AppServer.spireClient != nil {
		err = errors.Join(err, r.AppServer.spireClient.Close())
	}
	if r.AppServer.rateLimiter != nil {
		r.AppServer.rateLimiter.Stop()
	}
	if r.AppServer.db != nil {
		err = errors.Join(err, r.AppServer.db.Close())
	}
	if r.AppServer.audit != nil {
		err = errors.Join(err, r.AppServer.audit.Reconfigure(auditlog.AuditConfig{}))
	}
	return err
}

// buildServerTLSConfigWithWatcher creates a TLS config that uses the certificate watcher
// for dynamic certificate reloading.
func buildServerTLSConfigWithWatcher(cfg *ServerTLSConfig, cw *middleware.CertWatcher) (*tls.Config, error) {
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
