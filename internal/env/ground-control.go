package env

import (
	"strconv"
	"time"
)

type GroundControl struct {
	Harbor         Harbor
	Server         Server
	EmbeddedSPIRE  EmbeddedSPIRE
	Database       Database       `envPrefix:"DB_"`
	PasswordPolicy PasswordPolicy `envPrefix:"PASSWORD_"`
	SPIFFE         SPIFFE         `envPrefix:"SPIFFE_"`
	SPIRE          SPIRE          `envPrefix:"SPIRE_"`
	TLS            TLS            `envPrefix:"TLS_"`
	Audit          Audit          `envPrefix:"AUDIT_"`
}

type Database struct {
	Database string `env:"DATABASE"`
	Password string `env:"PASSWORD"`
	Username string `env:"USERNAME"`
	Port     string `env:"PORT"`
	Host     string `env:"HOST"`
}

type Harbor struct {
	URL               string `env:"HARBOR_URL"`
	Username          string `env:"HARBOR_USERNAME"`
	Password          string `env:"HARBOR_PASSWORD"`
	SkipHealthCheck   bool   `env:"SKIP_HARBOR_HEALTH_CHECK"`
	RobotDurationDays string `env:"ROBOT_DURATION_DAYS"      envDefault:"30"`
}

func (h Harbor) RobotDurationDaysValue() int64 {
	days, err := strconv.ParseInt(h.RobotDurationDays, 10, 64)
	if err != nil || days <= 0 {
		return 30
	}
	return days
}

type Server struct {
	Port            int           `env:"PORT"`
	AdminPassword   string        `env:"ADMIN_PASSWORD"`
	SessionDuration time.Duration `env:"SESSION_DURATION" envDefault:"24h"`
	LockoutDuration time.Duration `env:"LOCKOUT_DURATION" envDefault:"5m"`
	StaleThreshold  time.Duration `env:"STALE_THRESHOLD"  envDefault:"1h"`
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
	Enabled        bool   `env:"ENABLED"         envDefault:"false"`
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
