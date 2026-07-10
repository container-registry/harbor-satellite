package logger

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// capturedRequest is one export observed by the capture server.
type capturedRequest struct {
	path        string
	contentType string
	body        []byte
}

// newCaptureServer starts an HTTP server that records every request and
// responds with status.
func newCaptureServer(t *testing.T, status int) (*httptest.Server, chan capturedRequest) {
	t.Helper()
	ch := make(chan capturedRequest, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		ch <- capturedRequest{path: r.URL.Path, contentType: r.Header.Get("Content-Type"), body: body}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)

	return srv, ch
}

// mustNewOTelTransport builds a transport against endpoint, failing the test
// on error and closing the transport on cleanup.
func mustNewOTelTransport(t *testing.T, endpoint string) *otelTransport {
	t.Helper()
	tr, err := newOTelTransport(OTelConfig{Enabled: true, Endpoint: endpoint})
	if err != nil {
		t.Fatalf("newOTelTransport: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	return tr
}

// fullTestRecord returns a Record with every field populated.
func fullTestRecord() Record {
	return Record{
		EventID:   "evt-1",
		Timestamp: time.Date(2026, 6, 12, 10, 0, 0, 123456789, time.UTC),
		Component: ComponentGroundControl,
		EventType: "session.login.failure",
		AuditEvent: AuditEvent{
			Operation:    OpLogin,
			ResourceType: ResSession,
			Outcome:      OutcomeFailure,
			Severity:     SeverityWarning,
			Actor:        "admin",
			ActorType:    ActorUser,
			SourceIP:     "::1",
			UserAgent:    "curl/8.19.0",
			RequestID:    "req-1",
			SatelliteID:  "sat-1",
			Resource:     "admin",
			Reason:       ReasonBadPassword,
			Details:      map[string]any{"k": "v"},
		},
	}
}

// attrMap indexes OTLP attributes by key for assertions.
func attrMap(attrs []otlpKeyValue) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		m[a.Key] = a.Value.StringValue
	}

	return m
}

// decodeSingleLogRecord unmarshals an OTLP payload, asserts the envelope
// (exactly one resource carrying wantService, one scope, one record) and
// returns that record.
func decodeSingleLogRecord(t *testing.T, body []byte, wantService string) otlpLogRecord {
	t.Helper()
	var p otlpPayload
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(p.ResourceLogs) != 1 || len(p.ResourceLogs[0].ScopeLogs) != 1 || len(p.ResourceLogs[0].ScopeLogs[0].LogRecords) != 1 {
		t.Fatalf("payload shape = %+v, want 1 resource / 1 scope / 1 record", p)
	}
	if got := attrMap(p.ResourceLogs[0].Resource.Attributes)["service.name"]; got != wantService {
		t.Errorf("service.name = %q, want %q", got, wantService)
	}
	if name := p.ResourceLogs[0].ScopeLogs[0].Scope.Name; name != otelScopeName {
		t.Errorf("scope name = %q, want %q", name, otelScopeName)
	}

	return p.ResourceLogs[0].ScopeLogs[0].LogRecords[0]
}

// assertLogRecordCore checks timestamps, severity and the canonical JSON body.
func assertLogRecordCore(t *testing.T, lr otlpLogRecord, rec Record) {
	t.Helper()
	wantTS := strconv.FormatInt(rec.Timestamp.UnixNano(), 10)
	if lr.TimeUnixNano != wantTS || lr.ObservedTimeUnixNano != wantTS {
		t.Errorf("timeUnixNano = %q/%q, want %q", lr.TimeUnixNano, lr.ObservedTimeUnixNano, wantTS)
	}
	if lr.SeverityNumber != 13 || lr.SeverityText != "warning" {
		t.Errorf("severity = %d/%q, want 13/warning", lr.SeverityNumber, lr.SeverityText)
	}
	wantBody, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	if lr.Body.StringValue != string(wantBody) {
		t.Errorf("body = %q, want canonical record JSON %q", lr.Body.StringValue, wantBody)
	}
}

// assertSemanticAttributes checks the full attribute mapping produced for
// fullTestRecord.
func assertSemanticAttributes(t *testing.T, attrs map[string]string) {
	t.Helper()
	want := map[string]string{
		"event.name":                 "session.login.failure",
		"log.record.uid":             "evt-1",
		"user.name":                  "admin",
		"client.address":             "::1",
		"user_agent.original":        "curl/8.19.0",
		"error.type":                 "bad_password",
		"harbor.audit.operation":     "login",
		"harbor.audit.resource_type": "session",
		"harbor.audit.outcome":       "failure",
		"harbor.audit.actor_type":    "user",
		"harbor.audit.resource":      "admin",
		"harbor.audit.request_id":    "req-1",
		"harbor.audit.satellite_id":  "sat-1",
	}
	for k, v := range want {
		if attrs[k] != v {
			t.Errorf("attribute %s = %q, want %q", k, attrs[k], v)
		}
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(attrs["harbor.audit.details"]), &details); err != nil || details["k"] != "v" {
		t.Errorf("harbor.audit.details = %q, want JSON with k=v (err %v)", attrs["harbor.audit.details"], err)
	}
}

func TestOTelEmit_SendsWellFormedOTLP(t *testing.T) {
	srv, ch := newCaptureServer(t, http.StatusOK)
	tr := mustNewOTelTransport(t, srv.URL)

	rec := fullTestRecord()
	if err := tr.Emit(rec); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	got := <-ch

	if got.path != "/v1/logs" || got.contentType != "application/json" {
		t.Errorf("request = %q %q, want /v1/logs application/json", got.path, got.contentType)
	}
	lr := decodeSingleLogRecord(t, got.body, string(ComponentGroundControl))
	assertLogRecordCore(t, lr, rec)
	assertSemanticAttributes(t, attrMap(lr.Attributes))
}

func TestOTelEmit_OmitsEmptyOptionalAttributes(t *testing.T) {
	srv, ch := newCaptureServer(t, http.StatusOK)
	tr := mustNewOTelTransport(t, srv.URL)

	rec := Record{
		EventID:   "evt-2",
		Timestamp: time.Now().UTC(),
		Component: ComponentSatellite,
		EventType: "config.update.success",
		AuditEvent: AuditEvent{
			Operation:    OpUpdate,
			ResourceType: ResConfig,
			Outcome:      OutcomeSuccess,
			Severity:     SeverityInfo,
		},
	}
	if err := tr.Emit(rec); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	got := <-ch

	attrs := attrMap(decodeSingleLogRecord(t, got.body, string(ComponentSatellite)).Attributes)
	for _, absent := range []string{"user.name", "client.address", "user_agent.original", "error.type", "harbor.audit.details", "harbor.audit.satellite_id"} {
		if _, ok := attrs[absent]; ok {
			t.Errorf("attribute %s present on minimal record, want omitted", absent)
		}
	}
	for _, present := range []string{"event.name", "log.record.uid", "harbor.audit.operation", "harbor.audit.resource_type", "harbor.audit.outcome"} {
		if _, ok := attrs[present]; !ok {
			t.Errorf("attribute %s missing on minimal record, want present", present)
		}
	}
}

func TestOTelEmit_ErrorOnNon2xxStatus(t *testing.T) {
	srv, ch := newCaptureServer(t, http.StatusInternalServerError)
	tr := mustNewOTelTransport(t, srv.URL)

	err := tr.Emit(fullTestRecord())
	<-ch
	if err == nil {
		t.Fatal("Emit returned nil for a 500 response, want error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want it to mention the 500 status", err)
	}
}

func TestNewOTelTransport_NormalizesEndpoint(t *testing.T) {
	srv, _ := newCaptureServer(t, http.StatusOK)

	tr := mustNewOTelTransport(t, srv.URL)
	if tr.endpoint != srv.URL+"/v1/logs" {
		t.Errorf("endpoint = %q, want %q", tr.endpoint, srv.URL+"/v1/logs")
	}

	custom := mustNewOTelTransport(t, srv.URL+"/custom/logs")
	if custom.endpoint != srv.URL+"/custom/logs" {
		t.Errorf("endpoint = %q, want explicit path preserved", custom.endpoint)
	}
}

func TestNewOTelTransport_RejectsBadEndpoint(t *testing.T) {
	for _, endpoint := range []string{"", "not-a-url", "udp://127.0.0.1:4318"} {
		if _, err := newOTelTransport(OTelConfig{Enabled: true, Endpoint: endpoint}); err == nil {
			t.Errorf("newOTelTransport(%q) = nil error, want failure", endpoint)
		}
	}
}

func TestNewOTelTransport_FailsFastWhenUnreachable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	if _, err := newOTelTransport(OTelConfig{Enabled: true, Endpoint: "http://" + addr}); err == nil {
		t.Fatal("newOTelTransport to a closed port returned nil error, want failure")
	}
}

func TestOTelSeverityNumber(t *testing.T) {
	cases := []struct {
		severity Severity
		want     int
	}{
		{SeverityInfo, 9},
		{SeverityWarning, 13},
		{SeverityError, 17},
		{SeverityCritical, 21},
		{Severity("bogus"), 9},
	}
	for _, c := range cases {
		if got := otelSeverityNumber(c.severity); got != c.want {
			t.Errorf("otelSeverityNumber(%q) = %d, want %d", c.severity, got, c.want)
		}
	}
}

func TestAuditLogger_EmitsThroughOTelTransport(t *testing.T) {
	srv, ch := newCaptureServer(t, http.StatusOK)

	a, err := NewAuditLogger(AuditConfig{
		Enabled: true,
		OTel:    OTelConfig{Enabled: true, Endpoint: srv.URL},
	}, ComponentGroundControl)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}

	a.Log(AuditEvent{
		Operation:    OpLogin,
		ResourceType: ResSession,
		Outcome:      OutcomeFailure,
		Actor:        "admin",
		ActorType:    ActorUser,
		Reason:       ReasonBadPassword,
	})

	got := <-ch
	attrs := attrMap(decodeSingleLogRecord(t, got.body, string(ComponentGroundControl)).Attributes)
	if attrs["event.name"] != "session.login.failure" {
		t.Errorf("event.name = %q, want session.login.failure", attrs["event.name"])
	}
	if attrs["user.name"] != "admin" {
		t.Errorf("user.name = %q, want admin", attrs["user.name"])
	}
}
