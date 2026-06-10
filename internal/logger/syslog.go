package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"

	"gopkg.in/natefinch/lumberjack.v2"
)

// SyslogTarget selects where the syslog transport sends its messages. The wire
// format (RFC 5424) is identical for all three; only the sink differs.
type SyslogTarget string

const (
	// SyslogTargetDaemon hands messages to the local syslog daemon (e.g. rsyslog
	// or journald) over a unix socket. The daemon owns the file and its rotation.
	SyslogTargetDaemon SyslogTarget = "daemon"
	// SyslogTargetNetwork sends messages to a remote syslog/SIEM endpoint over
	// udp or tcp. There is no local file.
	SyslogTargetNetwork SyslogTarget = "network"
	// SyslogTargetFile writes messages to a local rotated file. We own the file,
	// so the rotation settings apply here (and only here).
	SyslogTargetFile SyslogTarget = "file"
)

const (
	// defaultSyslogTag is the RFC 5424 APP-NAME when none is configured.
	defaultSyslogTag = "harbor-audit"
	// defaultSyslogSocket is the conventional local syslog socket path.
	defaultSyslogSocket = "/dev/log"
	// facilityLocal0 is the syslog facility used for audit messages. local0 is
	// the conventional choice for application logs and the easiest for a SIEM to
	// route on. PRI = facility*8 + severity.
	facilityLocal0 = 16
	// rfc5424Time is RFC 3339 with microsecond precision. RFC 5424 permits at
	// most six fractional-second digits, so the event's nanosecond timestamp is
	// truncated to microseconds for the syslog header (the JSON body keeps the
	// full-precision timestamp).
	rfc5424Time = "2006-01-02T15:04:05.000000Z07:00"
	// nilValue is the RFC 5424 NILVALUE for an absent header field.
	nilValue = "-"
)

// SyslogFileConfig controls the rotated file used by SyslogTargetFile. These
// settings only apply to the file target: for the daemon target the OS rotates,
// and for the network target there is no file.
type SyslogFileConfig struct {
	Path       string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
}

// SyslogConfig configures the syslog transport. Target picks one of three sinks;
// the fields for the other two are ignored.
type SyslogConfig struct {
	Enabled bool
	Target  SyslogTarget

	// Tag is the RFC 5424 APP-NAME (defaults to defaultSyslogTag).
	Tag string

	// SocketPath is the local daemon socket for SyslogTargetDaemon
	// (defaults to defaultSyslogSocket).
	SocketPath string

	// Network ("udp" or "tcp") and Address ("host:port") apply to
	// SyslogTargetNetwork.
	Network string
	Address string

	// File applies to SyslogTargetFile.
	File SyslogFileConfig
}

// syslogTransport formats each event as an RFC 5424 message carrying the
// canonical Record JSON as its MSG, and writes it to a single sink chosen at
// construction. Because it serialises the same Record as every other transport,
// the syslog copy and the file copy can never disagree about an event.
type syslogTransport struct {
	w        io.Writer
	closer   io.Closer
	hostname string
	appName  string
	procID   string
	facility int
}

// newSyslogTransport builds a syslogTransport for cfg.Target. The sink is opened
// up front so an unreachable daemon / endpoint / unwritable file fails fast at
// startup rather than silently dropping events.
func newSyslogTransport(cfg SyslogConfig) (*syslogTransport, error) {
	tag := cfg.Tag
	if tag == "" {
		tag = defaultSyslogTag
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = nilValue
	}

	t := &syslogTransport{
		hostname: host,
		appName:  tag,
		procID:   strconv.Itoa(os.Getpid()),
		facility: facilityLocal0,
	}

	if err := t.openSink(cfg); err != nil {
		return nil, err
	}
	return t, nil
}

// openSink opens the io.Writer for cfg.Target and stores it on t.
func (t *syslogTransport) openSink(cfg SyslogConfig) error {
	switch cfg.Target {
	case SyslogTargetNetwork:
		conn, err := net.Dial(cfg.Network, cfg.Address)
		if err != nil {
			return fmt.Errorf("dial syslog endpoint %s/%s: %w", cfg.Network, cfg.Address, err)
		}
		t.w, t.closer = conn, conn
	case SyslogTargetFile:
		if err := ensureWritable(cfg.File.Path); err != nil {
			return fmt.Errorf("syslog file destination not writable: %w", err)
		}
		rotator := &lumberjack.Logger{
			Filename:   cfg.File.Path,
			MaxSize:    cfg.File.MaxSizeMB,
			MaxBackups: cfg.File.MaxBackups,
			MaxAge:     cfg.File.MaxAgeDays,
			Compress:   cfg.File.Compress,
		}
		t.w, t.closer = rotator, rotator
	case SyslogTargetDaemon:
		conn, err := dialDaemon(cfg.SocketPath)
		if err != nil {
			return err
		}
		t.w, t.closer = conn, conn
	default:
		return fmt.Errorf("unknown syslog target %q", cfg.Target)
	}
	return nil
}

// dialDaemon connects to the local syslog socket, trying datagram then stream
// since different daemons expose different socket types at the same path.
func dialDaemon(path string) (net.Conn, error) {
	if path == "" {
		path = defaultSyslogSocket
	}
	var lastErr error
	for _, network := range []string{"unixgram", "unix"} {
		conn, err := net.Dial(network, path)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("dial syslog daemon at %q: %w", path, lastErr)
}

// Emit formats the record as an RFC 5424 frame and writes it. The MSG is the
// canonical Record JSON; MSGID is left as NILVALUE because event_type can exceed
// the 32-character MSGID limit and is already carried in the body.
func (t *syslogTransport) Emit(r Record) error {
	body, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal audit record: %w", err)
	}
	pri := t.facility*8 + severityCode(r.Severity)
	// <PRI>VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID STRUCTURED-DATA MSG
	line := fmt.Sprintf("<%d>1 %s %s %s %s %s %s %s\n",
		pri,
		r.Timestamp.Format(rfc5424Time),
		t.hostname,
		t.appName,
		t.procID,
		nilValue, // MSGID
		nilValue, // STRUCTURED-DATA
		body,
	)
	if _, err := io.WriteString(t.w, line); err != nil {
		return fmt.Errorf("write syslog message: %w", err)
	}
	return nil
}

// Close releases the sink (socket connection or file rotator).
func (t *syslogTransport) Close() error {
	if t.closer != nil {
		return t.closer.Close()
	}
	return nil
}

// severityCode maps our Severity onto the RFC 5424 severity number. PRI then
// combines it with the facility so a SIEM can alert on level without parsing the
// message body.
func severityCode(s Severity) int {
	switch s {
	case SeverityCritical:
		return 2
	case SeverityError:
		return 3
	case SeverityWarning:
		return 4
	case SeverityInfo:
		return 6
	default:
		return 6
	}
}
