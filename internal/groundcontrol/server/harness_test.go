package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbor"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbortest"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/stretchr/testify/require"
)

const harnessAdminPassword = "Adm1nPassw0rd"

// harborStub serves every Harbor call of this test binary. GC's Harbor
// client is a process-wide sync.Once, so it can only ever point at one URL.
var harborStub *harbortest.Stub

func startHarborStub() *harbortest.Stub {
	stub := harbortest.Start()
	previous := env.GC
	env.GC.Harbor = env.Harbor{URL: stub.URL(), Username: harbortest.Username, Password: harbortest.Password}
	harbor.GetClient()
	env.GC = previous
	return stub
}

// gcHarness is a Ground Control built by NewServer on a fresh database,
// served over HTTP, with Harbor replaced by harborStub.
type gcHarness struct {
	t      *testing.T
	db     *dbtest.DB
	harbor *harbortest.Stub
	app    *Server
	// handler is the full production handler, for requests that need a
	// hand-crafted TLS state.
	handler http.Handler
	url     string
	// admin is a session of the bootstrap admin. Sessions keep the cost of
	// an admin request independent of how Basic auth evolves.
	admin authFunc
}

// newGCHarness needs TEST_DATABASE_URL and skips without it. Set extra GC
// environment variables with t.Setenv before calling it.
func newGCHarness(t *testing.T) *gcHarness {
	t.Helper()
	db := dbtest.New(t)
	harborStub.Reset()
	t.Cleanup(harborStub.Reset)

	for k, v := range map[string]string{
		"DB_HOST":                  db.Host,
		"DB_PORT":                  db.Port,
		"DB_USERNAME":              db.User,
		"DB_PASSWORD":              db.Password,
		"DB_DATABASE":              db.Name,
		"HARBOR_URL":               harborStub.URL(),
		"HARBOR_USERNAME":          harbortest.Username,
		"HARBOR_PASSWORD":          harbortest.Password,
		"SKIP_HARBOR_HEALTH_CHECK": "false",
		"ADMIN_PASSWORD":           harnessAdminPassword,
		"SPIFFE_SERVER_ENABLED":    "false",
		"EMBEDDED_SPIRE_ENABLED":   "false",
		"SPIRE_SERVER_SOCKET":      "",
		"AUDIT_LOG_ENABLED":        "false",
		"TLS_CERT_FILE":            "",
		"TLS_KEY_FILE":             "",
	} {
		t.Setenv(k, v)
	}
	loadGCForTest(t)

	res, err := NewServer(context.Background())
	require.NoError(t, err)
	ts := httptest.NewServer(res.Server.Handler)
	t.Cleanup(func() {
		ts.Close()
		require.NoError(t, res.Close())
	})

	h := &gcHarness{t: t, db: db, harbor: harborStub, app: res.AppServer, handler: res.Server.Handler, url: ts.URL}
	var session LoginResponse
	h.mustDo(http.MethodPost, "/login", LoginRequest{Username: "admin", Password: harnessAdminPassword}, nil, http.StatusOK).decode(t, &session)
	h.admin = bearerAuth(session.Token)
	return h
}

type authFunc func(*http.Request)

func basicAuth(user, password string) authFunc {
	return func(r *http.Request) { r.SetBasicAuth(user, password) }
}

func bearerAuth(token string) authFunc {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

var adminBasicAuth = basicAuth("admin", harnessAdminPassword)

type response struct {
	Status int
	Body   []byte
}

func (r response) decode(t *testing.T, v any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(r.Body, v), string(r.Body))
}

func (h *gcHarness) newRequest(method, path string, body any, auth authFunc) *http.Request {
	h.t.Helper()
	var reader io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		reader = bytes.NewBufferString(b)
	default:
		data, err := json.Marshal(b)
		require.NoError(h.t, err)
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, h.url+path, reader)
	require.NoError(h.t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth != nil {
		auth(req)
	}
	return req
}

// do sends a request to the GC under test. A nil body sends none; a string
// is sent as is; anything else is encoded as JSON.
func (h *gcHarness) do(method, path string, body any, auth authFunc) response {
	h.t.Helper()
	r, err := send(h.newRequest(method, path, body, auth))
	require.NoError(h.t, err)
	return r
}

// send is safe to call outside the test goroutine.
func send(req *http.Request) (response, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return response{}, err
	}
	data, err := io.ReadAll(resp.Body)
	if cerr := resp.Body.Close(); err == nil {
		err = cerr
	}
	return response{Status: resp.StatusCode, Body: data}, err
}

func (h *gcHarness) mustDo(method, path string, body any, auth authFunc, want int) response {
	h.t.Helper()
	r := h.do(method, path, body, auth)
	require.Equal(h.t, want, r.Status, "%s %s: %s", method, path, r.Body)
	return r
}

func testAppConfig() map[string]any {
	return map[string]any{
		"app_config": map[string]any{
			"log_level":                   "info",
			"state_replication_interval":  "@every 00h00m30s",
			"register_satellite_interval": "@every 00h00m05s",
			"heartbeat_interval":          "@every 00h00m30s",
		},
	}
}

func (h *gcHarness) createConfig(name string) {
	h.t.Helper()
	h.mustDo(http.MethodPost, "/api/configs", map[string]any{"config_name": name, "config": testAppConfig()}, h.admin, http.StatusCreated)
}

// syncGroup creates or replaces a group with one artifact per repository;
// the project of each artifact is the first path segment of its repository.
func (h *gcHarness) syncGroup(name string, repositories ...string) {
	h.t.Helper()
	artifacts := make([]GroupArtifact, 0, len(repositories))
	for _, repo := range repositories {
		artifacts = append(artifacts, GroupArtifact{
			Repository: repo,
			Tag:        []string{"latest"},
			Type:       "image",
			Digest:     "sha256:0000000000000000000000000000000000000000000000000000000000000001",
		})
	}
	h.mustDo(http.MethodPost, "/api/groups/sync", GroupSyncRequest{Group: name, Artifacts: artifacts}, h.admin, http.StatusOK)
}

// registerSatellite registers a token satellite and returns its ZTR token.
func (h *gcHarness) registerSatellite(name, config string, groups ...string) string {
	h.t.Helper()
	r := h.mustDo(http.MethodPost, "/api/satellites", TokenSatelliteRegistrationRequest{Name: name, ConfigName: config, Groups: groups}, h.admin, http.StatusOK)
	var tok TokenSatelliteRegistrationResponse
	r.decode(h.t, &tok)
	require.NotEmpty(h.t, tok.Token)
	return tok.Token
}

func (h *gcHarness) ztr(token string) StateConfigResponse {
	h.t.Helper()
	r := h.mustDo(http.MethodPost, "/satellites/ztr", ZTRRequest{Token: token}, nil, http.StatusOK)
	var cfg StateConfigResponse
	r.decode(h.t, &cfg)
	return cfg
}

func heartbeatRequest(name string) SatelliteStatusRequest {
	return SatelliteStatusRequest{
		Name:                name,
		Activity:            "replicating",
		StateReportInterval: "@every 00h00m30s",
		RequestCreatedTime:  time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		CPUPercent:          12.5,
		MemoryUsedBytes:     1 << 20,
		StorageUsedBytes:    1 << 30,
		ImageCount:          2,
		CachedImages: []CachedImageReport{
			{Reference: "library/alpine:3.20", SizeBytes: 3 << 20},
			{Reference: "library/busybox:1.36", SizeBytes: 1 << 20},
		},
	}
}

// onboard registers a satellite in groups and runs its ZTR, returning the
// robot credentials the satellite would use for heartbeats.
func (h *gcHarness) onboard(name, config string, groups ...string) RegistryCredentials {
	h.t.Helper()
	return h.ztr(h.registerSatellite(name, config, groups...)).Auth
}
