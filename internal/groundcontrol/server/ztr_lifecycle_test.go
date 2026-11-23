package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/container-registry/harbor-satellite/internal/env"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	auditlog "github.com/container-registry/harbor-satellite/internal/groundcontrol/logger"
	"github.com/stretchr/testify/require"
)

var (
	satelliteCols = []string{"id", "name", "created_at", "updated_at", "last_seen", "heartbeat_interval"}
	robotCols     = []string{"id", "robot_name", "robot_secret_hash", "robot_id", "satellite_id", "robot_expiry", "created_at", "updated_at"}
	configCols    = []string{"id", "config_name", "registry_url", "config", "created_at", "updated_at"}
)

func newFakeHarborServer(t *testing.T, satelliteName string, now time.Time) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2.0/robots":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodHead && r.URL.Path == "/api/v2.0/projects":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/robots"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"id":42,"name":"robot$%s","secret":"harbor-secret","expires_at":%d}`, satelliteName, now.Add(24*time.Hour).Unix())))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v2.0/robots/42":
			_, _ = w.Write([]byte(`{"secret":"refreshed-secret"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2.0/robots/42":
			_, _ = w.Write([]byte(fmt.Sprintf(`{"id":42,"name":"robot$%s","expires_at":%d}`, satelliteName, now.Add(48*time.Hour).Unix())))
		default:
			t.Errorf("unexpected Harbor request: %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
}

type ztrTestCtx struct {
	t             *testing.T
	server        *Server
	mock          sqlmock.Sqlmock
	now           time.Time
	createdAt     time.Time
	satelliteName string
	configName    string
	configData    []byte
	token         string
}

func TestZTRTokenLifecycle(t *testing.T) {
	const (
		satelliteName = "edge-01"
		configName    = "default"
		token         = "issued-ztr-token"
	)

	now := time.Now()
	harborServer := newFakeHarborServer(t, satelliteName, now)
	defer harborServer.Close()

	previousHarborConfig := env.GC.Harbor
	env.GC.Harbor.URL = harborServer.URL
	env.GC.Harbor.Username = "admin"
	env.GC.Harbor.Password = "password"
	env.GC.Harbor.SkipHealthCheck = false
	t.Cleanup(func() {
		env.GC.Harbor = previousHarborConfig
	})

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	audit, err := auditlog.NewAuditLogger(auditlog.AuditConfig{}, auditlog.ComponentGroundControl)
	require.NoError(t, err)

	ctx := &ztrTestCtx{
		t:             t,
		server:        &Server{db: db, dbQueries: database.New(db), audit: audit},
		mock:          mock,
		now:           now,
		createdAt:     now.Add(-time.Minute),
		satelliteName: satelliteName,
		configName:    configName,
		configData:    []byte(`{"setting":"value"}`),
		token:         token,
	}

	ctx.runRegistration()
	ctx.runZTRExchange()
	ctx.runZTRReplay()
}

func (ctx *ztrTestCtx) runRegistration() {
	ctx.mock.ExpectBegin()
	ctx.mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO satellites")).
		WithArgs(ctx.satelliteName).
		WillReturnRows(sqlmock.NewRows(satelliteCols).AddRow(int32(1), ctx.satelliteName, ctx.createdAt, ctx.now, nil, nil))
	ctx.mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO robot_accounts")).
		WithArgs("robot$"+ctx.satelliteName, sqlmock.AnyArg(), "42", int32(1), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(robotCols).AddRow(int32(1), "robot$"+ctx.satelliteName, "hash", "42", int32(1), nil, ctx.createdAt, ctx.now))
	ctx.mock.ExpectQuery(regexp.QuoteMeta("SELECT id, config_name, registry_url, config, created_at, updated_at FROM configs")).
		WithArgs(ctx.configName).
		WillReturnRows(sqlmock.NewRows(configCols).AddRow(int32(1), ctx.configName, "", ctx.configData, ctx.createdAt, ctx.now))
	ctx.mock.ExpectExec(regexp.QuoteMeta("INSERT INTO satellite_configs")).
		WithArgs(int32(1), int32(1)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	ctx.mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO satellite_token")).
		WithArgs(int32(1), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"token"}).AddRow(ctx.token))
	ctx.mock.ExpectCommit()

	registerBody, err := json.Marshal(TokenSatelliteRegistrationRequest{
		Name:       ctx.satelliteName,
		ConfigName: ctx.configName,
	})
	require.NoError(ctx.t, err)
	registerRequest := httptest.NewRequest(http.MethodPost, "/api/satellites", bytes.NewReader(registerBody))
	registerResponse := httptest.NewRecorder()
	ctx.server.RegisterSatellite(registerResponse, registerRequest)

	require.Equal(ctx.t, http.StatusOK, registerResponse.Code)
	var registration TokenSatelliteRegistrationResponse
	require.NoError(ctx.t, json.Unmarshal(registerResponse.Body.Bytes(), &registration))
	require.Equal(ctx.t, ctx.token, registration.Token)
}

func (ctx *ztrTestCtx) runZTRExchange() {
	robotExpiry := sql.NullTime{}

	// The first ZTR exchange succeeds and consumes the token.
	ctx.mock.ExpectQuery(regexp.QuoteMeta("SELECT id, satellite_id, token, created_at, updated_at, expires_at FROM satellite_token")).
		WithArgs(ctx.token).
		WillReturnRows(sqlmock.NewRows([]string{"id", "satellite_id", "token", "created_at", "updated_at", "expires_at"}).
			AddRow(int32(1), int32(1), ctx.token, ctx.createdAt, ctx.now, ctx.now.Add(time.Hour)))
	ctx.mock.ExpectQuery(regexp.QuoteMeta("SELECT id, robot_name, robot_secret_hash, robot_id, satellite_id, robot_expiry, created_at, updated_at FROM robot_accounts")).
		WithArgs(int32(1)).
		WillReturnRows(sqlmock.NewRows(robotCols).AddRow(int32(1), "robot$"+ctx.satelliteName, "hash", "42", int32(1), robotExpiry, ctx.createdAt, ctx.now))
	ctx.mock.ExpectExec(regexp.QuoteMeta("UPDATE robot_accounts")).
		WithArgs(int32(1), "robot$"+ctx.satelliteName, sqlmock.AnyArg(), "42", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	ctx.mock.ExpectQuery(regexp.QuoteMeta("SELECT satellite_id, group_id FROM satellite_groups")).
		WithArgs(int32(1)).
		WillReturnRows(sqlmock.NewRows([]string{"satellite_id", "group_id"}))
	ctx.mock.ExpectQuery(regexp.QuoteMeta("SELECT id, name, created_at, updated_at, last_seen, heartbeat_interval FROM satellites")).
		WithArgs(int32(1)).
		WillReturnRows(sqlmock.NewRows(satelliteCols).AddRow(int32(1), ctx.satelliteName, ctx.createdAt, ctx.now, nil, nil))
	ctx.mock.ExpectQuery(regexp.QuoteMeta("SELECT satellite_id, config_id FROM satellite_configs")).
		WithArgs(int32(1)).
		WillReturnRows(sqlmock.NewRows([]string{"satellite_id", "config_id"}).AddRow(int32(1), int32(1)))
	ctx.mock.ExpectQuery(regexp.QuoteMeta("SELECT id, config_name, registry_url, config, created_at, updated_at FROM configs")).
		WithArgs(int32(1)).
		WillReturnRows(sqlmock.NewRows(configCols).AddRow(int32(1), ctx.configName, "", ctx.configData, ctx.createdAt, ctx.now))
	ctx.mock.ExpectExec(regexp.QuoteMeta("DELETE FROM satellite_token")).
		WithArgs(ctx.token).
		WillReturnResult(sqlmock.NewResult(0, 1))

	ztrBody, err := json.Marshal(ZTRRequest{Token: ctx.token})
	require.NoError(ctx.t, err)
	ztrRequest := httptest.NewRequest(http.MethodPost, "/satellites/ztr", bytes.NewReader(ztrBody))
	ztrResponse := httptest.NewRecorder()
	ctx.server.Ztr(ztrResponse, ztrRequest)

	require.Equal(ctx.t, http.StatusOK, ztrResponse.Code)
	var stateConfig StateConfigResponse
	require.NoError(ctx.t, json.Unmarshal(ztrResponse.Body.Bytes(), &stateConfig))
	require.Equal(ctx.t, "robot$"+ctx.satelliteName, stateConfig.Auth.Username)
	require.Equal(ctx.t, "refreshed-secret", stateConfig.Auth.Password)
}

func (ctx *ztrTestCtx) runZTRReplay() {
	// A second exchange cannot find the consumed token.
	ctx.mock.ExpectQuery(regexp.QuoteMeta("SELECT id, satellite_id, token, created_at, updated_at, expires_at FROM satellite_token")).
		WithArgs(ctx.token).
		WillReturnError(sql.ErrNoRows)

	ztrBody, err := json.Marshal(ZTRRequest{Token: ctx.token})
	require.NoError(ctx.t, err)
	reusedRequest := httptest.NewRequest(http.MethodPost, "/satellites/ztr", bytes.NewReader(ztrBody))
	reusedResponse := httptest.NewRecorder()
	ctx.server.Ztr(reusedResponse, reusedRequest)

	require.Equal(ctx.t, http.StatusBadRequest, reusedResponse.Code)
	require.Contains(ctx.t, reusedResponse.Body.String(), "Invalid Token")
	require.NoError(ctx.t, ctx.mock.ExpectationsWereMet())
}
