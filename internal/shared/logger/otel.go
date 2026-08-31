package logger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// The OTel transport exports each Record as an OpenTelemetry log over OTLP/HTTP
// (JSON encoding, POST {endpoint}/v1/logs). It is hand-rolled on net/http for
// the same reason the syslog transport is hand-rolled instead of using
// log/syslog: the official OTel SDK would pull a large dependency tree into
// both modules and its asynchronous batching does not fit the synchronous
// Emit(Record) seam. OTLP/JSON is a stable wire format and the subset needed
// for log export is small.
//
// Field mapping follows OTel semantic conventions where one exists
// (event.name, log.record.uid, user.name, client.address, user_agent.original,
// error.type, service.name) and the harbor.audit.* namespace for fields that
// have no standard name. The log body carries the canonical Record JSON, so
// this transport can never disagree with the syslog transport about what an
// event looks like.

const (
	// otlpLogsPath is the standard OTLP/HTTP path for log export; it is
	// appended to the configured endpoint when the endpoint has no path.
	otlpLogsPath = "/v1/logs"
	// otelScopeName identifies the instrumentation scope on every export.
	otelScopeName = "harbor-satellite/audit"
	// otelExportTimeout bounds one export round-trip. Emit runs synchronously
	// on the caller's request path, so a hung collector must fail fast and
	// surface as a transport breadcrumb instead of stalling the request.
	otelExportTimeout = 5 * time.Second
	// otelProbeTimeout bounds the startup reachability probe.
	otelProbeTimeout = 3 * time.Second
)

// OTelConfig configures the OTLP/HTTP log export transport. Endpoint is the
// collector base URL (e.g. "http://127.0.0.1:4318"); the standard /v1/logs
// path is appended when the URL carries no path of its own.
type OTelConfig struct {
	Enabled  bool
	Endpoint string
}

// otelTransport posts each record to an OTLP/HTTP logs endpoint.
type otelTransport struct {
	client   *http.Client
	endpoint string
}

// newOTelTransport validates and normalises the endpoint and verifies it is
// reachable now, so a misconfigured or down collector fails fast at startup
// (mirroring the syslog transport's up-front dial) instead of advertising
// audit logging while dropping every event.
func newOTelTransport(cfg OTelConfig) (*otelTransport, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse otel endpoint %q: %w", cfg.Endpoint, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("otel endpoint %q must be an http(s) URL", cfg.Endpoint)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = otlpLogsPath
	}
	if err := probeOTelEndpoint(u); err != nil {
		return nil, err
	}
	return &otelTransport{
		client:   &http.Client{Timeout: otelExportTimeout},
		endpoint: u.String(),
	}, nil
}

// probeOTelEndpoint checks TCP reachability of the collector. HTTP has no
// persistent connection to fail on at construction the way a TCP syslog dial
// does, so reachability is probed explicitly.
func probeOTelEndpoint(u *url.URL) error {
	host := u.Host
	if u.Port() == "" {
		port := "80"
		if u.Scheme == "https" {
			port = "443"
		}
		host = net.JoinHostPort(u.Hostname(), port)
	}
	conn, err := net.DialTimeout("tcp", host, otelProbeTimeout)
	if err != nil {
		return fmt.Errorf("otel endpoint %q not reachable: %w", u.Host, err)
	}
	return conn.Close()
}

// Emit exports the record as a single OTLP/HTTP JSON request. A non-2xx
// response is an error so the caller leaves a breadcrumb instead of silently
// losing the event.
func (t *otelTransport) Emit(r Record) error {
	payload, err := otlpPayloadFor(r)
	if err != nil {
		return fmt.Errorf("marshal otlp payload: %w", err)
	}
	resp, err := t.client.Post(t.endpoint, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("export otlp logs: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, 4096)); err != nil {
		return fmt.Errorf("read otlp response body: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("otlp endpoint returned status %d", resp.StatusCode)
	}
	return nil
}

// Close releases pooled HTTP connections.
func (t *otelTransport) Close() error {
	t.client.CloseIdleConnections()
	return nil
}

// Minimal OTLP/JSON shapes (proto3 JSON mapping of the OTLP logs request).
// Only string-valued attributes are needed: every audit field is a string and
// details is carried as a JSON string.

type otlpValue struct {
	StringValue string `json:"stringValue"`
}

type otlpKeyValue struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

type otlpLogRecord struct {
	// TimeUnixNano is a string because proto3 JSON encodes 64-bit integers
	// as decimal strings.
	TimeUnixNano         string         `json:"timeUnixNano"`
	ObservedTimeUnixNano string         `json:"observedTimeUnixNano"`
	SeverityNumber       int            `json:"severityNumber"`
	SeverityText         string         `json:"severityText"`
	Body                 otlpValue      `json:"body"`
	Attributes           []otlpKeyValue `json:"attributes"`
}

type otlpScope struct {
	Name string `json:"name"`
}

type otlpScopeLogs struct {
	Scope      otlpScope       `json:"scope"`
	LogRecords []otlpLogRecord `json:"logRecords"`
}

type otlpResource struct {
	Attributes []otlpKeyValue `json:"attributes"`
}

type otlpResourceLogs struct {
	Resource  otlpResource    `json:"resource"`
	ScopeLogs []otlpScopeLogs `json:"scopeLogs"`
}

type otlpPayload struct {
	ResourceLogs []otlpResourceLogs `json:"resourceLogs"`
}

// otlpPayloadFor maps one Record onto an OTLP logs request. The component
// becomes the resource's service.name so collectors group events by emitting
// service, exactly like any other instrumented service.
func otlpPayloadFor(r Record) ([]byte, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	ts := strconv.FormatInt(r.Timestamp.UnixNano(), 10)
	rec := otlpLogRecord{
		TimeUnixNano:         ts,
		ObservedTimeUnixNano: ts,
		SeverityNumber:       otelSeverityNumber(r.Severity),
		SeverityText:         string(r.Severity),
		Body:                 otlpValue{StringValue: string(body)},
		Attributes:           otelAttributes(r),
	}
	p := otlpPayload{ResourceLogs: []otlpResourceLogs{{
		Resource: otlpResource{Attributes: []otlpKeyValue{
			{Key: "service.name", Value: otlpValue{StringValue: string(r.Component)}},
		}},
		ScopeLogs: []otlpScopeLogs{{
			Scope:      otlpScope{Name: otelScopeName},
			LogRecords: []otlpLogRecord{rec},
		}},
	}}}
	return json.Marshal(p)
}

// otelAttributes flattens the record into OTLP attributes: semantic-convention
// names where OTel defines one, harbor.audit.* for the rest. Empty optional
// fields are omitted, matching the canonical JSON's omitempty behaviour.
func otelAttributes(r Record) []otlpKeyValue {
	attrs := make([]otlpKeyValue, 0, 14)
	attrs = appendOTelAttr(attrs, "event.name", r.EventType)
	attrs = appendOTelAttr(attrs, "log.record.uid", r.EventID)
	attrs = appendOTelAttr(attrs, "user.name", r.Actor)
	attrs = appendOTelAttr(attrs, "client.address", r.SourceIP)
	attrs = appendOTelAttr(attrs, "user_agent.original", r.UserAgent)
	attrs = appendOTelAttr(attrs, "error.type", string(r.Reason))
	attrs = appendOTelAttr(attrs, "harbor.audit.operation", string(r.Operation))
	attrs = appendOTelAttr(attrs, "harbor.audit.resource_type", string(r.ResourceType))
	attrs = appendOTelAttr(attrs, "harbor.audit.outcome", string(r.Outcome))
	attrs = appendOTelAttr(attrs, "harbor.audit.actor_type", string(r.ActorType))
	attrs = appendOTelAttr(attrs, "harbor.audit.resource", r.Resource)
	attrs = appendOTelAttr(attrs, "harbor.audit.request_id", r.RequestID)
	attrs = appendOTelAttr(attrs, "harbor.audit.satellite_id", r.SatelliteID)
	attrs = appendOTelAttr(attrs, "harbor.audit.details", detailsJSON(r.Details))
	return attrs
}

// appendOTelAttr appends a string attribute, skipping empty values so the
// attribute list mirrors the canonical JSON's omitempty semantics.
func appendOTelAttr(attrs []otlpKeyValue, key, value string) []otlpKeyValue {
	if value == "" {
		return attrs
	}
	return append(attrs, otlpKeyValue{Key: key, Value: otlpValue{StringValue: value}})
}

// detailsJSON renders the free-form details map as a JSON string attribute.
// An empty map yields "" so the attribute is omitted entirely.
func detailsJSON(d map[string]any) string {
	if len(d) == 0 {
		return ""
	}
	b, err := json.Marshal(d)
	if err != nil {
		return ""
	}
	return string(b)
}

// otelSeverityNumber maps our Severity onto the OTel SeverityNumber ranges
// (INFO=9, WARN=13, ERROR=17, FATAL=21), the counterpart of the syslog PRI
// mapping in severityCode.
func otelSeverityNumber(s Severity) int {
	switch s {
	case SeverityCritical:
		return 21
	case SeverityError:
		return 17
	case SeverityWarning:
		return 13
	case SeverityInfo:
		return 9
	default:
		return 9
	}
}
