package server

import (
	"context"
	"crypto/tls"
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
)

type Server struct {
	port            int
	db              *sql.DB
	dbQueries       *database.Queries
	passwordPolicy  auth.PasswordPolicy
	sessionDuration time.Duration
	lockoutDuration time.Duration
}

// TLS configuration exported for main.go
var (
	TLSEnabled bool
	TLSCertPath string
	TLSKeyPath  string
)

const (
	defaultSessionDurationHours = 24
	defaultLockoutDurationMins  = 15
)

var (
	dbName   = os.Getenv("DB_DATABASE")
	password = os.Getenv("DB_PASSWORD")
	username = os.Getenv("DB_USERNAME")
	PORT     = os.Getenv("DB_PORT")
	HOST     = os.Getenv("DB_HOST")
)

func loadSessionDuration() time.Duration {
	hours := defaultSessionDurationHours
	if envVal := os.Getenv("SESSION_DURATION_HOURS"); envVal != "" {
		if parsed, err := strconv.Atoi(envVal); err == nil && parsed > 0 {
			hours = parsed
		}
	}
	return time.Duration(hours) * time.Hour
}

func loadLockoutDuration() time.Duration {
	mins := defaultLockoutDurationMins
	if envVal := os.Getenv("LOCKOUT_DURATION_MINUTES"); envVal != "" {
		if parsed, err := strconv.Atoi(envVal); err == nil && parsed > 0 {
			mins = parsed
		}
	}
	return time.Duration(mins) * time.Minute
}

func NewServer(ctx context.Context) *http.Server {
	port, err := strconv.Atoi(os.Getenv("PORT"))
	if err != nil {
		log.Fatalf("PORT is not valid: %v", err)
	}

	// Load FIPS mode configuration
	fipsMode := os.Getenv("FIPS_MODE") == "true"
	auth.FIPSMode = fipsMode

	// Load DB SSL mode (default: disable for backward compatibility)
	sslmode := os.Getenv("DB_SSLMODE")
	if sslmode == "" {
		sslmode = "disable"
	}

	// Validate FIPS requirements for database connection
	if fipsMode && sslmode == "disable" {
		log.Fatalf("FIPS mode requires encrypted database connections (DB_SSLMODE must not be 'disable')")
	}

	connStr := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		username,
		password,
		HOST,
		PORT,
		dbName,
		sslmode,
	)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Error in sql: %v", err)
	}

	dbQueries := database.New(db)
	passwordPolicy := auth.LoadPolicyFromEnv()
	sessionDuration := loadSessionDuration()
	lockoutDuration := loadLockoutDuration()

	s := &Server{
		port:            port,
		db:              db,
		dbQueries:       dbQueries,
		passwordPolicy:  passwordPolicy,
		sessionDuration: sessionDuration,
		lockoutDuration: lockoutDuration,
	}

	// Bootstrap system admin user
	if err := s.BootstrapSystemAdmin(ctx); err != nil {
		log.Fatalf("Failed to bootstrap system admin: %v", err)
	}

	go s.StartCleanupJob(ctx, CleanupConfig{
		RetentionDays:   defaultRetentionDays,
		CleanupInterval: defaultCleanupInterval,
	})

	// Load TLS configuration
	TLSCertPath = os.Getenv("TLS_CERT_PATH")
	TLSKeyPath = os.Getenv("TLS_KEY_PATH")
	TLSEnabled = TLSCertPath != "" && TLSKeyPath != ""

	// Validate FIPS requirements for TLS
	if fipsMode && !TLSEnabled {
		log.Fatalf("FIPS mode requires TLS_CERT_PATH and TLS_KEY_PATH")
	}

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      s.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Configure TLS with FIPS-approved cipher suites
	if TLSEnabled {
		server.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		if fipsMode {
			server.TLSConfig.CipherSuites = []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			}
		}
	}

	return server
}

