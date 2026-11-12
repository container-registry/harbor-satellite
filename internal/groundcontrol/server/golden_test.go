package server

import (
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/golden from the responses of this run")

const goldenDir = "testdata/golden"

// goldenRecorder sends requests and compares each normalised response with
// testdata/golden/<name>.json, or rewrites the file with -update.
type goldenRecorder struct {
	t       *testing.T
	h       *gcHarness
	written map[string]bool
}

func (g *goldenRecorder) step(name, method, path string, body any, auth authFunc) response {
	g.t.Helper()
	r := g.h.do(method, path, body, auth)
	g.compare(name, map[string]any{
		"request": method + " " + path,
		"status":  r.Status,
		"body":    normalizeGoldenBody(r.Body, g.h.harbor.Host()),
	})
	return r
}

// artifact records the JSON file inside a state artifact GC pushed to Harbor.
func (g *goldenRecorder) artifact(name, repository string) {
	g.t.Helper()
	data, err := g.h.harbor.ArtifactFile(repository, "latest", "artifacts.json")
	require.NoError(g.t, err)
	g.compare(name, map[string]any{
		"artifact": repository + ":latest",
		"body":     normalizeGoldenBody(data, g.h.harbor.Host()),
	})
}

func (g *goldenRecorder) compare(name string, record map[string]any) {
	g.t.Helper()
	require.False(g.t, g.written[name], "duplicate golden step %q", name)
	g.written[name] = true
	got, err := json.MarshalIndent(record, "", "  ")
	require.NoError(g.t, err)
	got = append(got, '\n')
	file := filepath.Join(goldenDir, name+".json")
	if *updateGolden {
		require.NoError(g.t, os.WriteFile(file, got, 0o600))
		return
	}
	want, err := os.ReadFile(file)
	if !assert.NoError(g.t, err, "missing golden file; run go test ./internal/groundcontrol/server -run TestGoldenAPI -update") {
		return
	}
	assert.Equal(g.t, string(want), string(got), "golden mismatch for %s", file)
}

// finish removes (with -update) or reports golden files no step produced.
func (g *goldenRecorder) finish() {
	g.t.Helper()
	entries, err := os.ReadDir(goldenDir)
	require.NoError(g.t, err)
	var stale []string
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".json")
		if g.written[name] {
			continue
		}
		if *updateGolden {
			require.NoError(g.t, os.Remove(filepath.Join(goldenDir, e.Name())))
			continue
		}
		stale = append(stale, e.Name())
	}
	sort.Strings(stale)
	assert.Empty(g.t, stale, "golden files that no step produced")
}

// normalizeGoldenBody replaces what differs between runs: timestamps, row
// and robot IDs, secrets, tokens and the stub's host:port.
func normalizeGoldenBody(body []byte, harborHost string) any {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return strings.ReplaceAll(string(body), harborHost, "harbor.test")
	}
	return normalizeGoldenValue("", v, harborHost)
}

func normalizeGoldenValue(key string, v any, harborHost string) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			x[k] = normalizeGoldenValue(k, val, harborHost)
		}
		return x
	case []any:
		for i, val := range x {
			x[i] = normalizeGoldenValue(key, val, harborHost)
		}
		return x
	case string:
		switch {
		case x == "":
			return x
		case isSecretKey(key):
			return "<secret>"
		case isIDKey(key) && isInteger(x):
			return "<id>"
		case isTimestamp(x):
			return "<timestamp>"
		default:
			return strings.ReplaceAll(x, harborHost, "harbor.test")
		}
	case float64:
		if isIDKey(key) {
			return "<id>"
		}
		return x
	default:
		return v
	}
}

func isSecretKey(key string) bool {
	switch strings.ToLower(strings.ReplaceAll(key, "_", "")) {
	case "token", "password", "secret", "passwordhash", "robotsecrethash", "jointoken":
		return true
	default:
		return false
	}
}

func isIDKey(key string) bool {
	for _, suffix := range []string{"ID", "Id", "_id", "IDs", "Ids", "_ids"} {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return key == "id"
}

func isInteger(s string) bool {
	_, err := strconv.ParseInt(s, 10, 64)
	return err == nil
}

func isTimestamp(s string) bool {
	_, err := time.Parse(time.RFC3339Nano, s)
	return err == nil
}

// TestGoldenAPI pins the responses of every Ground Control endpoint, and the
// state artifacts satellites read, against real Postgres and a Harbor stub.
// It records today's behaviour, defects included, so refactors can prove
// they change nothing they did not mean to.
func TestGoldenAPI(t *testing.T) {
	h := newGCHarness(t)
	g := &goldenRecorder{t: t, h: h, written: map[string]bool{}}

	g.step("ping", http.MethodGet, "/ping", nil, nil)
	g.step("health", http.MethodGet, "/health", nil, nil)
	g.step("login-wrong-password", http.MethodPost, "/login", LoginRequest{Username: "admin", Password: "Wrong-passw0rd"}, nil)
	login := g.step("login", http.MethodPost, "/login", LoginRequest{Username: "admin", Password: harnessAdminPassword}, nil)
	var session LoginResponse
	login.decode(t, &session)
	admin := bearerAuth(session.Token)
	g.step("api-without-credentials", http.MethodGet, "/api/configs", nil, nil)

	g.step("spire-status", http.MethodGet, "/api/spire/status", nil, admin)
	g.step("spire-agents", http.MethodGet, "/api/spire/agents", nil, admin)
	g.step("satellites-register-spiffe", http.MethodPost, "/api/satellites/register", SPIFFESatelliteRegistrationRequest{
		SatelliteName: "spiffe-01", AttestationMethod: JoinToken, Selectors: []string{"unix:uid:0"},
	}, admin)

	g.step("users-create", http.MethodPost, "/api/users", CreateUserRequest{Username: "operator", Password: "Operat0rPass"}, admin)
	g.step("users-list", http.MethodGet, "/api/users", nil, admin)
	g.step("users-get", http.MethodGet, "/api/users/operator", nil, admin)
	g.step("users-get-missing", http.MethodGet, "/api/users/nobody", nil, admin)
	g.step("users-set-password", http.MethodPatch, "/api/users/operator/password", ChangeUserPasswordRequest{NewPassword: "Operat0rPass2"}, admin)
	g.step("users-change-own-password", http.MethodPatch, "/api/users/password",
		ChangePasswordRequest{CurrentPassword: "Operat0rPass2", NewPassword: "Operat0rPass3"}, basicAuth("operator", "Operat0rPass2"))
	g.step("users-create-as-admin-role", http.MethodPost, "/api/users",
		CreateUserRequest{Username: "intruder", Password: "Intrud3rPass"}, basicAuth("operator", "Operat0rPass3"))

	edgeConfig := map[string]any{"config_name": "edge-config", "config": testAppConfig()}
	g.step("configs-create", http.MethodPost, "/api/configs", edgeConfig, admin)
	g.step("configs-create-duplicate", http.MethodPost, "/api/configs", edgeConfig, admin)
	g.step("configs-create-second", http.MethodPost, "/api/configs", map[string]any{"config_name": "edge-config-2", "config": testAppConfig()}, admin)
	g.step("configs-list", http.MethodGet, "/api/configs", nil, admin)
	g.step("configs-get", http.MethodGet, "/api/configs/edge-config", nil, admin)
	g.step("configs-get-missing", http.MethodGet, "/api/configs/missing", nil, admin)
	g.step("configs-update", http.MethodPatch, "/api/configs/edge-config", map[string]any{"app_config": map[string]any{"log_level": "debug"}}, admin)

	g.step("groups-sync", http.MethodPost, "/api/groups/sync", GroupSyncRequest{Group: "edge-group", Artifacts: []GroupArtifact{
		{Repository: "library/alpine", Tag: []string{"3.20"}, Type: "image", Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111"},
		{Repository: "team/app", Tag: []string{"v1", "latest"}, Type: "image", Digest: "sha256:2222222222222222222222222222222222222222222222222222222222222222"},
	}}, admin)
	g.step("groups-sync-second", http.MethodPost, "/api/groups/sync", GroupSyncRequest{Group: "edge-group-2", Artifacts: []GroupArtifact{
		{Repository: "tools/cli", Tag: []string{"1.0"}, Type: "image", Digest: "sha256:3333333333333333333333333333333333333333333333333333333333333333"},
	}}, admin)
	g.step("groups-list", http.MethodGet, "/api/groups", nil, admin)
	g.step("groups-get", http.MethodGet, "/api/groups/edge-group", nil, admin)
	g.step("groups-get-missing", http.MethodGet, "/api/groups/missing", nil, admin)

	satellite := TokenSatelliteRegistrationRequest{Name: "edge-01", ConfigName: "edge-config", Groups: []string{"edge-group"}}
	registered := g.step("satellites-register", http.MethodPost, "/api/satellites", satellite, admin)
	var reg TokenSatelliteRegistrationResponse
	registered.decode(t, &reg)
	g.step("satellites-register-duplicate", http.MethodPost, "/api/satellites", satellite, admin)
	g.step("satellites-list", http.MethodGet, "/api/satellites", nil, admin)
	g.step("satellites-get", http.MethodGet, "/api/satellites/edge-01", nil, admin)
	g.step("groups-satellites", http.MethodGet, "/api/groups/edge-group/satellites", nil, admin)
	g.step("satellites-status-before-heartbeat", http.MethodGet, "/api/satellites/edge-01/status", nil, admin)

	g.step("ztr-unknown-token", http.MethodPost, "/satellites/ztr", ZTRRequest{Token: strings.Repeat("0", 32)}, nil)
	h.drainOutbox()
	ztr := g.step("ztr", http.MethodPost, "/satellites/ztr", ZTRRequest(reg), nil)
	var stateCfg StateConfigResponse
	ztr.decode(t, &stateCfg)
	g.step("ztr-reused-token", http.MethodPost, "/satellites/ztr", ZTRRequest(reg), nil)
	g.artifact("artifact-satellite-state", "satellite/satellite-state/edge-01/state")
	g.artifact("artifact-group-state", "satellite/group-state/edge-group/state")
	g.artifact("artifact-config-state", "satellite/config-state/edge-config/state")

	robot := basicAuth(stateCfg.Auth.Username, stateCfg.Auth.Password)
	g.step("sync-without-credentials", http.MethodPost, "/satellites/sync", heartbeatRequest("edge-01"), nil)
	g.step("sync", http.MethodPost, "/satellites/sync", heartbeatRequest("edge-01"), robot)
	g.step("satellites-status", http.MethodGet, "/api/satellites/edge-01/status", nil, admin)
	g.step("satellites-images", http.MethodGet, "/api/satellites/edge-01/images", nil, admin)
	g.step("satellites-active", http.MethodGet, "/api/satellites/active", nil, admin)
	g.step("satellites-stale", http.MethodGet, "/api/satellites/stale", nil, admin)
	if spiffeBuild {
		g.step("spiffe-ztr-without-svid", http.MethodGet, "/satellites/spiffe-ztr", nil, nil)
	} else {
		g.written["spiffe-ztr-without-svid"] = true
	}

	membership := SatelliteGroupRequest{Satellite: "edge-01", Group: "edge-group-2"}
	g.step("groups-add-satellite", http.MethodPost, "/api/groups/satellite", membership, admin)
	g.step("groups-add-satellite-again", http.MethodPost, "/api/groups/satellite", membership, admin)
	g.artifact("artifact-satellite-state-two-groups", "satellite/satellite-state/edge-01/state")
	g.step("groups-remove-satellite", http.MethodDelete, "/api/groups/satellite", membership, admin)
	g.step("configs-set-satellite", http.MethodPost, "/api/configs/satellite", SatelliteConfigRequest{Satellite: "edge-01", ConfigName: "edge-config-2"}, admin)
	g.step("configs-delete-in-use", http.MethodDelete, "/api/configs/edge-config-2", nil, admin)
	g.step("configs-delete", http.MethodDelete, "/api/configs/edge-config", nil, admin)
	g.step("groups-delete", http.MethodDelete, "/api/groups/edge-group-2", nil, admin)
	g.step("satellites-delete", http.MethodDelete, "/api/satellites/edge-01", nil, admin)
	g.step("satellites-get-deleted", http.MethodGet, "/api/satellites/edge-01", nil, admin)
	g.step("users-delete", http.MethodDelete, "/api/users/operator", nil, admin)
	g.step("logout", http.MethodPost, "/api/logout", nil, admin)
	g.step("api-after-logout", http.MethodGet, "/api/configs", nil, admin)

	g.finish()
}
