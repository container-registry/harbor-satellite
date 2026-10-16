package env

import (
	"fmt"
	"strconv"
	"time"
)

type GroundControl struct {
	Harbor         Harbor
	Server         Server
	EmbeddedSPIRE  EmbeddedSPIRE
	Database       Database       `envPrefix:"DB_"`
	Outbox         Outbox         `envPrefix:"OUTBOX_"`
	Policy         Policy         `envPrefix:"POLICY_"`
	OIDC           OIDC           `envPrefix:"OIDC_"`
	RobotRotation  RobotRotation  `envPrefix:"ROBOT_ROTATION_"`
	PasswordPolicy PasswordPolicy `envPrefix:"PASSWORD_"`
	SPIFFE         SPIFFE         `envPrefix:"SPIFFE_"`
	SPIRE          SPIRE          `envPrefix:"SPIRE_"`
	TLS            TLS            `envPrefix:"TLS_"`
	Audit          Audit          `envPrefix:"AUDIT_"`

	// DataEncryptionKey seals stored secrets (base64 of 32 bytes); required
	// only once robot rotation or webhooks are enabled (plan items 8.4, 9).
	DataEncryptionKey string `env:"DATA_ENCRYPTION_KEY"`
}

type Database struct {
	Database string `env:"DATABASE"`
	Password string `env:"PASSWORD"`
	Username string `env:"USERNAME"`
	Port     string `env:"PORT"`
	Host     string `env:"HOST"`
	// SSLMode and SSLRootCert are parsed but not used yet: URL() still sends
	// sslmode=disable. Plan 3.3 (G5) switches the default to "require".
	SSLMode     string `env:"SSLMODE"     envDefault:"disable"`
	SSLRootCert string `env:"SSLROOTCERT"`
	// Not used yet (plan 3.2): the API pool opens URL_API or the single DSN;
	// fleet and async open their own pool only when their URL is set.
	URLAPI   string `env:"URL_API"`
	URLFleet string `env:"URL_FLEET"`
	URLAsync string `env:"URL_ASYNC"`

	APIPool   Pool `envPrefix:"API_"`
	FleetPool Pool `envPrefix:"FLEET_"`
	AsyncPool Pool `envPrefix:"ASYNC_"`
}

// Pool tunes one database/sql pool; zero keeps the database/sql default.
type Pool struct {
	MaxOpenConns           int           `env:"MAX_OPEN_CONNS"`
	MaxIdleConns           int           `env:"MAX_IDLE_CONNS"`
	ConnMaxLifetime        time.Duration `env:"CONN_MAX_LIFETIME"`
	ConnMaxIdleTime        time.Duration `env:"CONN_MAX_IDLE_TIME"`
	StatementTimeout       time.Duration `env:"STATEMENT_TIMEOUT"`
	LockTimeout            time.Duration `env:"LOCK_TIMEOUT"`
	IdleInTxSessionTimeout time.Duration `env:"IDLE_IN_TX_SESSION_TIMEOUT"`
}

type Harbor struct {
	URL               string `env:"HARBOR_URL"`
	Username          string `env:"HARBOR_USERNAME"`
	Password          string `env:"HARBOR_PASSWORD"`
	SkipHealthCheck   bool   `env:"SKIP_HARBOR_HEALTH_CHECK"`
	RobotDurationDays string `env:"ROBOT_DURATION_DAYS"      envDefault:"30"`
	// Plan item 2 skeleton, parsed but not used yet: requests per second
	// the async plane sends to Harbor, and how often GC pings Harbor for
	// /health.
	RateLimit     float64       `env:"HARBOR_RATE_LIMIT"     envDefault:"20"`
	ProbeInterval time.Duration `env:"HARBOR_PROBE_INTERVAL" envDefault:"30s"`
}

func (h Harbor) RobotDurationDaysValue() int64 {
	days, err := strconv.ParseInt(h.RobotDurationDays, 10, 64)
	if err != nil || days <= 0 {
		return 30
	}
	return days
}

// Outbox tunes the publisher of the async plane (plan item 2.3); parsed but
// not used yet.
type Outbox struct {
	BatchSize    int           `env:"BATCH_SIZE"    envDefault:"100"`
	PollInterval time.Duration `env:"POLL_INTERVAL" envDefault:"1s"`
	Concurrency  int           `env:"CONCURRENCY"   envDefault:"4"`
	MaxAttempts  int           `env:"MAX_ATTEMPTS"  envDefault:"10"`
	BackoffBase  time.Duration `env:"BACKOFF_BASE"  envDefault:"1s"`
	BackoffMax   time.Duration `env:"BACKOFF_MAX"   envDefault:"5m"`
	Lease        time.Duration `env:"LEASE"         envDefault:"1m"`
}

const (
	PolicyEngineBuiltin = "builtin"
	PolicyEngineCasbin  = "casbin"
)

// Policy selects the /api route check (plan item 7.2): builtin is today's
// system_admin check, casbin the Casbin policy, which polls
// policy_version every PollInterval for stored changes.
type Policy struct {
	Engine       string        `env:"ENGINE"        envDefault:"builtin"`
	PollInterval time.Duration `env:"POLL_INTERVAL" envDefault:"10s"`
}

// Validate rejects an unknown engine, so a typo cannot silently run
// builtin, and a poll interval that is not positive.
func (p Policy) Validate() error {
	if p.Engine != PolicyEngineBuiltin && p.Engine != PolicyEngineCasbin {
		return fmt.Errorf("POLICY_ENGINE %q: want %s or %s", p.Engine, PolicyEngineBuiltin, PolicyEngineCasbin)
	}
	if p.PollInterval <= 0 {
		return fmt.Errorf("POLICY_POLL_INTERVAL %s: must be positive", p.PollInterval)
	}
	return nil
}

type Server struct {
	Port            int           `env:"PORT"             envDefault:"8080"`
	AdminPassword   string        `env:"ADMIN_PASSWORD"`
	SessionDuration time.Duration `env:"SESSION_DURATION" envDefault:"24h"`
	LockoutDuration time.Duration `env:"LOCKOUT_DURATION" envDefault:"5m"`
	// StaleThreshold is how long a satellite without an @every heartbeat
	// interval may stay silent before it is stale (plan item 4.3, A8);
	// parsed but not used yet.
	StaleThreshold time.Duration `env:"STALE_THRESHOLD" envDefault:"15m"`
	// D6 skeleton: the FailureLimiter is built from these but never blocks yet.
	AuthFailureLimit  int           `env:"AUTH_FAILURE_LIMIT"  envDefault:"20"`
	AuthFailureWindow time.Duration `env:"AUTH_FAILURE_WINDOW" envDefault:"1m"`

	// 6.5 skeleton: parsed, not enforced yet. POST /satellites/sync answers
	// 413 for a larger body, a journal upload included.
	SyncMaxBodyBytes int64 `env:"SYNC_MAX_BODY_BYTES" envDefault:"4194304"`

	FleetHotPath
}

// FleetHotPath configures plan item 4 (ADR-0016): satellite sessions, the
// heartbeat buffer, the lifecycle jobs, status partitions and the split
// rate limits. Parsed but not used yet, except that RATE_LIMIT_SPLIT
// builds the split limiters, which answer 501 until 4.6. The rates are strings ("<requests>/<duration>")
// because env cannot import the middleware that parses them.
type FleetHotPath struct {
	SatelliteSessionTTL       time.Duration `env:"SATELLITE_SESSION_TTL"        envDefault:"720h"`
	SatelliteSessionCacheTTL  time.Duration `env:"SATELLITE_SESSION_CACHE_TTL"  envDefault:"30s"`
	SatelliteSessionCacheSize int           `env:"SATELLITE_SESSION_CACHE_SIZE" envDefault:"10000"`
	// SatelliteRobotAuth keeps robot Basic auth on the satellite endpoints
	// next to sessions and SVIDs.
	SatelliteRobotAuth        bool          `env:"SATELLITE_ROBOT_AUTH"         envDefault:"true"`
	HeartbeatFlushInterval    time.Duration `env:"HEARTBEAT_FLUSH_INTERVAL"     envDefault:"5s"`
	HeartbeatBufferMax        int           `env:"HEARTBEAT_BUFFER_MAX"         envDefault:"10000"`
	HeartbeatMaxFlushFailures int           `env:"HEARTBEAT_MAX_FLUSH_FAILURES" envDefault:"3"`
	StaleCheckInterval        time.Duration `env:"STALE_CHECK_INTERVAL"         envDefault:"1m"`
	SessionExpiryInterval     time.Duration `env:"SESSION_EXPIRY_INTERVAL"      envDefault:"1h"`
	StatusRetentionDays       int           `env:"STATUS_RETENTION_DAYS"        envDefault:"7"`
	StatusPartitionsAhead     int           `env:"STATUS_PARTITIONS_AHEAD"      envDefault:"3"`
	RateLimitSplit            bool          `env:"RATE_LIMIT_SPLIT"             envDefault:"false"`
	RateLimitAnonymous        string        `env:"RATE_LIMIT_ANONYMOUS"         envDefault:"10/1m"`
	// RateLimitFleet: 3 per the 1s heartbeat floor of normalizeHeartbeatInterval.
	RateLimitFleet string `env:"RATE_LIMIT_FLEET" envDefault:"3/1s"`
}

// OIDC configures operator login (plan item 8.1, ADR-0018). Parsed but not
// used yet. RoleMap maps a provider group to admin or system_admin
// ("group=role,..."); a user whose groups map to no role cannot log in.
type OIDC struct {
	Enabled              bool          `env:"ENABLED"                envDefault:"false"`
	IssuerURL            string        `env:"ISSUER_URL"`
	ClientID             string        `env:"CLIENT_ID"`
	ClientSecret         string        `env:"CLIENT_SECRET"`
	RedirectURL          string        `env:"REDIRECT_URL"`
	Scopes               []string      `env:"SCOPES"                 envDefault:"openid,profile,email,groups"`
	UsernameClaim        string        `env:"USERNAME_CLAIM"         envDefault:"preferred_username"`
	GroupsClaim          string        `env:"GROUPS_CLAIM"           envDefault:"groups"`
	DisablePasswordLogin bool          `env:"DISABLE_PASSWORD_LOGIN" envDefault:"false"`
	LoginStateTTL        time.Duration `env:"LOGIN_STATE_TTL"        envDefault:"10m"`

	RoleMap map[string]string `env:"ROLE_MAP" envKeyValSeparator:"="`
}

// Validate rejects an OIDC_ROLE_MAP role other than admin or system_admin.
// A group whose name contains "," or "=" cannot be mapped: those separate
// the entries.
func (o OIDC) Validate() error {
	for group, role := range o.RoleMap {
		if role != "admin" && role != "system_admin" {
			return fmt.Errorf("OIDC_ROLE_MAP %s=%s: want admin or system_admin", group, role)
		}
	}
	return nil
}

// RobotRotation configures the Harbor 2.x robot rotation of plan item 8.4.
// Parsed but not used yet.
type RobotRotation struct {
	Enabled      bool          `env:"ENABLED"       envDefault:"false"`
	BeforeExpiry time.Duration `env:"BEFORE_EXPIRY" envDefault:"72h"`
	Interval     time.Duration `env:"INTERVAL"      envDefault:"10m"`
	CacheRefresh time.Duration `env:"CACHE_REFRESH" envDefault:"30s"`
}

type PasswordPolicy struct {
	MinLength        int  `env:"MIN_LENGTH"        envDefault:"8"`
	MaxLength        int  `env:"MAX_LENGTH"        envDefault:"128"`
	RequireUppercase bool `env:"REQUIRE_UPPERCASE" envDefault:"true"`
	RequireLowercase bool `env:"REQUIRE_LOWERCASE" envDefault:"true"`
	RequireNumber    bool `env:"REQUIRE_NUMBER"    envDefault:"true"`
	RequireSpecial   bool `env:"REQUIRE_SPECIAL"   envDefault:"false"`
}

type SPIFFE struct {
	Enabled        bool   `env:"SERVER_ENABLED"  envDefault:"false"`
	TrustDomain    string `env:"TRUST_DOMAIN"    envDefault:"harbor-satellite.local"`
	Provider       string `env:"PROVIDER"        envDefault:"sidecar"`
	EndpointSocket string `env:"ENDPOINT_SOCKET" envDefault:"unix:///run/spire/sockets/agent.sock"`
	CertFile       string `env:"CERT_FILE"`
	KeyFile        string `env:"KEY_FILE"`
	BundleFile     string `env:"BUNDLE_FILE"`
}

type SPIRE struct {
	DataDir       string `env:"DATA_DIR"       envDefault:"/tmp/spire-data"`
	TrustDomain   string `env:"TRUST_DOMAIN"   envDefault:"harbor-satellite.local"`
	BindAddress   string `env:"BIND_ADDRESS"   envDefault:"127.0.0.1"`
	ServerAddress string `env:"SERVER_ADDRESS" envDefault:"spire-server"`
	ServerPort    int    `env:"SERVER_PORT"    envDefault:"8081"`
	ServerSocket  string `env:"SERVER_SOCKET"`
}

type EmbeddedSPIRE struct {
	Enabled bool `env:"EMBEDDED_SPIRE_ENABLED" envDefault:"false"`
}

type TLS struct {
	CertFile string `env:"CERT_FILE"`
	KeyFile  string `env:"KEY_FILE"`
	CAFile   string `env:"CA_FILE"`
}

func (t TLS) Enabled() bool {
	return t.CertFile != "" && t.KeyFile != ""
}

type Audit struct {
	SyslogAddress         string  `env:"SYSLOG_ADDRESS"`
	OTelEndpoint          string  `env:"OTEL_ENDPOINT"`
	SyslogFilePath        *string `env:"SYSLOG_FILE_PATH"`
	LogEnabled            bool    `env:"LOG_ENABLED"              envDefault:"false"`
	TrustForwardedHeaders bool    `env:"TRUST_FORWARDED_HEADERS"  envDefault:"false"`
	SyslogEnabled         bool    `env:"SYSLOG_ENABLED"           envDefault:"true"`
	SyslogTarget          string  `env:"SYSLOG_TARGET"            envDefault:"file"`
	SyslogTag             string  `env:"SYSLOG_TAG"               envDefault:"harbor-audit"`
	SyslogSocketPath      string  `env:"SYSLOG_SOCKET_PATH"       envDefault:"/dev/log"`
	SyslogNetwork         string  `env:"SYSLOG_NETWORK"           envDefault:"udp"`
	SyslogFileMaxSizeMB   int     `env:"SYSLOG_FILE_MAX_SIZE_MB"  envDefault:"100"`
	SyslogFileMaxBackups  int     `env:"SYSLOG_FILE_MAX_BACKUPS"  envDefault:"7"`
	SyslogFileMaxAgeDays  int     `env:"SYSLOG_FILE_MAX_AGE_DAYS" envDefault:"30"`
	SyslogFileCompress    bool    `env:"SYSLOG_FILE_COMPRESS"     envDefault:"true"`
}
