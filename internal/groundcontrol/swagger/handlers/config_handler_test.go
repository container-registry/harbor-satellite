package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	swaggermodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/models"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/configs"
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/stretchr/testify/require"
)

func TestListConfigs(t *testing.T) {
	t.Run("returns configs", func(t *testing.T) {
		mock := newMockHandlerService(t)
		now := time.Now().UTC().Truncate(time.Second)
		rows := sqlmock.NewRows([]string{"id", "config_name", "registry_url", "config", "created_at", "updated_at"}).
			AddRow(1, "edge", "https://registry.example", []byte(`{"app_config":{"log_level":"info"}}`), now, now).
			AddRow(2, "factory", "https://registry.example", []byte(`{"zot_config":{}}`), now, now)
		mock.ExpectQuery("SELECT id, config_name, registry_url, config, created_at, updated_at FROM configs").WillReturnRows(rows)

		responder := ListConfigs(configs.ListConfigsParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/configs"),
		}, handlerTestPrincipal)

		response, ok := responder.(*configs.ListConfigsOK)
		require.True(t, ok)
		require.Len(t, response.Payload, 2)
		require.Equal(t, "edge", response.Payload[0].ConfigName)
		config, ok := response.Payload[0].Config.(map[string]any)
		require.True(t, ok)
		appConfig, ok := config["app_config"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "info", appConfig["log_level"])
	})

	t.Run("returns an empty array", func(t *testing.T) {
		mock := newMockHandlerService(t)
		mock.ExpectQuery("SELECT id, config_name, registry_url, config, created_at, updated_at FROM configs").
			WillReturnRows(sqlmock.NewRows([]string{"id", "config_name", "registry_url", "config", "created_at", "updated_at"}))

		responder := ListConfigs(configs.ListConfigsParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/configs"),
		}, handlerTestPrincipal)

		response, ok := responder.(*configs.ListConfigsOK)
		require.True(t, ok)
		require.NotNil(t, response.Payload)
		require.Empty(t, response.Payload)
	})

	t.Run("reports database failures", func(t *testing.T) {
		mock := newMockHandlerService(t)
		mock.ExpectQuery("SELECT id, config_name, registry_url, config, created_at, updated_at FROM configs").
			WillReturnError(errors.New("database unavailable"))

		responder := ListConfigs(configs.ListConfigsParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/configs"),
		}, handlerTestPrincipal)

		_, ok := responder.(*configs.ListConfigsInternalServerError)
		require.True(t, ok)
	})
}

func TestGetConfig(t *testing.T) {
	t.Run("returns a matching config", func(t *testing.T) {
		mock := newMockHandlerService(t)
		now := time.Now().UTC().Truncate(time.Second)
		mock.ExpectQuery("SELECT id, config_name, registry_url, config, created_at, updated_at FROM configs").
			WithArgs("edge").
			WillReturnRows(sqlmock.NewRows([]string{"id", "config_name", "registry_url", "config", "created_at", "updated_at"}).
				AddRow(1, "edge", "https://registry.example", []byte(`{"app_config":{}}`), now, now))

		responder := GetConfig(configs.GetConfigParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/configs/edge"),
			Config:      "edge",
		}, handlerTestPrincipal)

		response, ok := responder.(*configs.GetConfigOK)
		require.True(t, ok)
		require.Equal(t, "edge", response.Payload.ConfigName)
		require.Equal(t, int32(1), response.Payload.ID)
	})

	t.Run("returns not found", func(t *testing.T) {
		mock := newMockHandlerService(t)
		mock.ExpectQuery("SELECT id, config_name, registry_url, config, created_at, updated_at FROM configs").
			WithArgs("missing").
			WillReturnError(sql.ErrNoRows)

		responder := GetConfig(configs.GetConfigParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/api/configs/missing"),
			Config:      "missing",
		}, handlerTestPrincipal)

		_, ok := responder.(*configs.GetConfigNotFound)
		require.True(t, ok)
	})
}

func TestCreateConfigRejectsInvalidInput(t *testing.T) {
	newMockHandlerService(t)

	responder := CreateConfig(configs.CreateConfigParams{
		HTTPRequest: handlerRequest(http.MethodPost, "/api/configs"),
	}, handlerTestPrincipal)
	_, ok := responder.(*configs.CreateConfigBadRequest)
	require.True(t, ok)

	responder = CreateConfig(configs.CreateConfigParams{
		HTTPRequest: handlerRequest(http.MethodPost, "/api/configs"),
		Body:        &swaggermodels.APIConfigObject{ConfigName: "Invalid Name"},
	}, handlerTestPrincipal)
	_, ok = responder.(*configs.CreateConfigBadRequest)
	require.True(t, ok)
}

func TestDeleteConfigRejectsConfigInUse(t *testing.T) {
	mock := newMockHandlerService(t)
	now := time.Now().UTC().Truncate(time.Second)
	mock.ExpectQuery("SELECT id, config_name, registry_url, config, created_at, updated_at FROM configs").
		WithArgs("edge").
		WillReturnRows(sqlmock.NewRows([]string{"id", "config_name", "registry_url", "config", "created_at", "updated_at"}).
			AddRow(1, "edge", "https://registry.example", []byte(`{}`), now, now))
	mock.ExpectQuery("SELECT satellite_id, config_id FROM satellite_configs").
		WithArgs(int32(1)).
		WillReturnRows(sqlmock.NewRows([]string{"satellite_id", "config_id"}).AddRow(5, 1))

	responder := DeleteConfig(configs.DeleteConfigParams{
		HTTPRequest: handlerRequest(http.MethodDelete, "/api/configs/edge"),
		Config:      "edge",
	}, handlerTestPrincipal)

	response, ok := responder.(*configs.DeleteConfigBadRequest)
	require.True(t, ok)
	require.Equal(t, "Cannot delete config that is in use", response.Payload.Message)
}

func TestConfigMutationHandlersRejectInvalidInput(t *testing.T) {
	newMockHandlerService(t)

	_, setBadRequest := SetSatelliteConfig(configs.SetSatelliteConfigParams{
		HTTPRequest: handlerRequest(http.MethodPost, "/api/configs/satellite"),
	}, handlerTestPrincipal).(*configs.SetSatelliteConfigBadRequest)
	require.True(t, setBadRequest)

	_, updateBadBody := UpdateConfig(configs.UpdateConfigParams{
		HTTPRequest: handlerRequest(http.MethodPatch, "/api/configs/edge"),
		Config:      "edge",
	}, handlerTestPrincipal).(*configs.UpdateConfigBadRequest)
	require.True(t, updateBadBody)

	_, updateBadName := UpdateConfig(configs.UpdateConfigParams{
		HTTPRequest: handlerRequest(http.MethodPatch, "/api/configs/Invalid%20Name"),
		Config:      "Invalid Name",
		Body:        &swaggermodels.APIConfigValue{},
	}, handlerTestPrincipal).(*configs.UpdateConfigBadRequest)
	require.True(t, updateBadName)
}

func TestCaptureConfigPatchBodyPreservesExplicitNull(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPatch, "/api/configs/example", strings.NewReader(`{"app_config":null}`))
	handler := CaptureConfigPatchBody(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var decoded swaggermodels.APIConfigValue
		require.NoError(t, json.NewDecoder(r.Body).Decode(&decoded))
		require.Nil(t, decoded.AppConfig)

		patch, err := configPatchJSON(r, &decoded)
		require.NoError(t, err)
		patched, err := jsonpatch.MergePatch(
			[]byte(`{"app_config":{"log_level":"info"},"state_config":{"state":"ref"}}`),
			patch,
		)
		require.NoError(t, err)
		require.JSONEq(t, `{"state_config":{"state":"ref"}}`, string(patched))
	}))

	handler.ServeHTTP(httptest.NewRecorder(), request)
}

func TestCaptureConfigPatchBodyRestoresBody(t *testing.T) {
	t.Parallel()

	const document = `{"zot_config":{"log":{"level":"debug"}}}`
	request := httptest.NewRequest(http.MethodPatch, "/api/configs/example", strings.NewReader(document))
	handler := CaptureConfigPatchBody(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var decoded map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&decoded))
		require.Contains(t, decoded, "zot_config")

		patch, err := configPatchJSON(r, &swaggermodels.APIConfigValue{})
		require.NoError(t, err)
		require.JSONEq(t, document, string(patch))
	}))

	handler.ServeHTTP(httptest.NewRecorder(), request)
}

func TestCaptureConfigPatchBodyRejectsOversizedPatch(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/configs/example",
		strings.NewReader(strings.Repeat("x", maxConfigPatchBytes+1)),
	)
	called := false
	handler := CaptureConfigPatchBody(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.False(t, called)
	var payload swaggermodels.AppError
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&payload))
	require.Equal(t, int64(http.StatusRequestEntityTooLarge), payload.Code)
	require.Contains(t, payload.Message, "10 MiB")
}

func TestCaptureConfigPatchBodySkipsMissingBody(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPatch, "/api/configs/example", nil)
	request.Body = nil
	handler := CaptureConfigPatchBody(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, captured := r.Context().Value(configPatchContextKey{}).(capturedConfigPatch)
		require.False(t, captured)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), request)
}

func TestConfigPatchJSONFallsBackToDecodedModel(t *testing.T) {
	t.Parallel()

	body := &swaggermodels.APIConfigValue{AppConfig: map[string]any{"log_level": "debug"}}
	patch, err := configPatchJSON(nil, body)
	require.NoError(t, err)
	require.JSONEq(t, `{"app_config":{"log_level":"debug"}}`, string(patch))
}

func TestCaptureConfigPatchBodyReportsReadFailure(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPatch, "/api/configs/example", nil)
	request.Body = failingReadCloser{}
	handler := CaptureConfigPatchBody(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, err := configPatchJSON(r, &swaggermodels.APIConfigValue{})
		require.ErrorContains(t, err, "read config merge patch")
	}))

	handler.ServeHTTP(httptest.NewRecorder(), request)
}

func TestValidateConfigPatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		patch   string
		wantErr string
	}{
		{name: "empty object", patch: `{}`},
		{name: "objects and null", patch: `{"app_config":null,"state_config":{},"zot_config":{"log":{"level":"debug"}}}`},
		{name: "unknown section", patch: `{"unknown":{}}`, wantErr: "unknown section"},
		{name: "scalar section", patch: `{"app_config":"invalid"}`, wantErr: "must be an object or null"},
		{name: "array section", patch: `{"state_config":[]}`, wantErr: "must be an object or null"},
		{name: "null document", patch: `null`, wantErr: "expected an object"},
		{name: "array document", patch: `[]`, wantErr: "cannot unmarshal"},
		{name: "malformed document", patch: `{`, wantErr: "unexpected end"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validateConfigPatch([]byte(test.patch))
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

type failingReadCloser struct{}

func (failingReadCloser) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func (failingReadCloser) Close() error {
	return nil
}
