package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type recordedRequest struct {
	Method, Path, Query, Authorization string
	Body                               map[string]any
}

// recordingServer answers every request with status and body and records
// what the CLI sent.
func recordingServer(t *testing.T, status int, body string) (*httptest.Server, func() []recordedRequest) {
	t.Helper()
	var mu sync.Mutex
	var requests []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		recorded := recordedRequest{
			Method: request.Method, Path: request.URL.Path, Query: request.URL.RawQuery,
			Authorization: request.Header.Get("Authorization"),
		}
		if len(raw) > 0 {
			require.NoError(t, json.Unmarshal(raw, &recorded.Body))
		}
		mu.Lock()
		requests = append(requests, recorded)
		mu.Unlock()
		if body != "" {
			writer.Header().Set("Content-Type", "application/json")
		}
		writer.WriteHeader(status)
		_, err = writer.Write([]byte(body))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	return server, func() []recordedRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedRequest(nil), requests...)
	}
}

func TestOperatorCommandsCallTheirRoutes(t *testing.T) {
	t.Setenv("GROUND_CONTROL_WEBHOOK_SECRET", "a-webhook-secret-of-at-least-32-bytes")
	tests := []struct {
		name   string
		args   []string
		status int
		// body is what the server answers; the generated client decodes it,
		// so list routes need an array.
		body string
		want recordedRequest
	}{
		{
			name: "revoke satellite", args: []string{"revoke", "satellite", "--name", "edge-1"}, status: http.StatusOK,
			want: recordedRequest{Method: http.MethodPost, Path: "/api/satellites/edge-1/revoke"},
		},
		{
			name: "unrevoke satellite", args: []string{"unrevoke", "satellite", "--name", "edge-1"}, status: http.StatusOK,
			want: recordedRequest{Method: http.MethodPost, Path: "/api/satellites/edge-1/unrevoke"},
		},
		{
			name: "satellite lifecycle", args: []string{"get", "satellite-lifecycle", "--satellite", "edge-1"}, status: http.StatusOK,
			want: recordedRequest{Method: http.MethodGet, Path: "/api/satellites/edge-1/lifecycle"},
		},
		{
			name: "list policies", args: []string{"get", "policies"}, status: http.StatusOK, body: `[{"id":1}]`,
			want: recordedRequest{Method: http.MethodGet, Path: "/api/policies"},
		},
		{
			name: "create policy",
			args: []string{
				"create", "policy", "--subject", "user:alice", "--effect", "deny",
				"--scope", "group:edge", "--resource-type", "satellite", "--action", "delete",
			},
			status: http.StatusCreated,
			want: recordedRequest{Method: http.MethodPost, Path: "/api/policies", Body: map[string]any{
				"subject": "user:alice", "effect": "deny", "scope": "group:edge", "resource_type": "satellite", "action": "delete",
			}},
		},
		{
			name: "create policy with server defaults", args: []string{"create", "policy", "--subject", "role:admin", "--effect", "allow"},
			status: http.StatusCreated,
			want: recordedRequest{Method: http.MethodPost, Path: "/api/policies", Body: map[string]any{
				"subject": "role:admin", "effect": "allow",
			}},
		},
		{
			name: "delete policy", args: []string{"delete", "policy", "--id", "42"}, status: http.StatusNoContent,
			want: recordedRequest{Method: http.MethodDelete, Path: "/api/policies/42"},
		},
		{
			name: "list role bindings", args: []string{"get", "role-bindings"}, status: http.StatusOK, body: `[{"id":1}]`,
			want: recordedRequest{Method: http.MethodGet, Path: "/api/role-bindings"},
		},
		{
			name: "create role binding", args: []string{"create", "role-binding", "--principal", "oidc:ops", "--role", "role:viewer"},
			status: http.StatusCreated,
			want: recordedRequest{Method: http.MethodPost, Path: "/api/role-bindings", Body: map[string]any{
				"principal": "oidc:ops", "role": "role:viewer",
			}},
		},
		{
			name: "delete role binding", args: []string{"delete", "role-binding", "--id", "7"}, status: http.StatusNoContent,
			want: recordedRequest{Method: http.MethodDelete, Path: "/api/role-bindings/7"},
		},
		{
			name: "list webhooks", args: []string{"get", "webhooks"}, status: http.StatusOK, body: `[{"id":1}]`,
			want: recordedRequest{Method: http.MethodGet, Path: "/api/webhooks"},
		},
		{
			name: "create webhook",
			args: []string{
				"create", "webhook", "--url", "https://hooks.example/fleet",
				"--event-type", "GroupChanged,SatelliteStale", "--event-type", "SatelliteRevoked",
			},
			status: http.StatusCreated,
			want: recordedRequest{Method: http.MethodPost, Path: "/api/webhooks", Body: map[string]any{
				"url": "https://hooks.example/fleet", "secret": "a-webhook-secret-of-at-least-32-bytes",
				"event_types": []any{"GroupChanged", "SatelliteStale", "SatelliteRevoked"},
			}},
		},
		{
			name: "create webhook for every kind", args: []string{"create", "webhook", "--url", "https://hooks.example/all"},
			status: http.StatusCreated,
			want: recordedRequest{Method: http.MethodPost, Path: "/api/webhooks", Body: map[string]any{
				"url": "https://hooks.example/all", "secret": "a-webhook-secret-of-at-least-32-bytes",
			}},
		},
		{
			name: "delete webhook", args: []string{"delete", "webhook", "--id", "3"}, status: http.StatusNoContent,
			want: recordedRequest{Method: http.MethodDelete, Path: "/api/webhooks/3"},
		},
		{
			name: "failed outbox events", args: []string{"get", "outbox-events", "--state", "failed", "--limit", "5"}, status: http.StatusOK, body: `[{"id":1}]`,
			want: recordedRequest{Method: http.MethodGet, Path: "/api/ops/outbox", Query: "limit=5&state=failed"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := tt.body
			if body == "" && tt.status != http.StatusNoContent {
				body = `{"name":"x"}`
			}
			server, requests := recordingServer(t, tt.status, body)
			args := append([]string{"--server", server.URL, "--token", "session-token"}, tt.args...)
			output, err := execute(t, args...)
			require.NoError(t, err)
			if body == "" {
				require.Equal(t, "204 No Content\n", output)
			} else {
				require.JSONEq(t, body, output)
			}

			got := requests()
			require.Len(t, got, 1)
			want := tt.want
			want.Authorization = "Bearer session-token"
			got[0].Query = sortedQuery(t, got[0].Query)
			require.Equal(t, want, got[0])
		})
	}
}

// sortedQuery drops empty parameters, which the generated client sends for
// unset optional flags, and orders the rest.
func sortedQuery(t *testing.T, raw string) string {
	t.Helper()
	values, err := url.ParseQuery(raw)
	require.NoError(t, err)
	for key, value := range values {
		if len(value) == 1 && (value[0] == "" || value[0] == "0") {
			values.Del(key)
		}
	}
	return values.Encode()
}

func TestOperatorCommandsShowServerErrors(t *testing.T) {
	t.Parallel()
	server, _ := recordingServer(t, http.StatusForbidden, `{"code":403,"message":"system administrator role is required"}`)
	_, err := execute(t, "--server", server.URL, "--token", "session-token", "get", "outbox-events")
	require.ErrorContains(t, err, "403 Forbidden")
	require.ErrorContains(t, err, "system administrator role is required")
}

func TestOperatorCommandsValidateBeforeCalling(t *testing.T) {
	t.Setenv("GROUND_CONTROL_WEBHOOK_SECRET", "")
	server, requests := recordingServer(t, http.StatusOK, `{}`)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"revoke without name", []string{"revoke", "satellite", "--name", " "}, "--name must not be empty"},
		{"policy effect", []string{"create", "policy", "--subject", "user:alice", "--effect", "maybe"}, "--effect must be allow or deny"},
		{"policy subject", []string{"create", "policy", "--subject", "", "--effect", "allow"}, "--subject must not be empty"},
		{"policy id", []string{"delete", "policy", "--id", "0"}, "--id must be a positive number"},
		{"role binding role", []string{"create", "role-binding", "--principal", "user:alice", "--role", ""}, "--role must not be empty"},
		{"webhook id", []string{"delete", "webhook", "--id", "-1"}, "--id must be a positive number"},
		{"webhook secret", []string{"create", "webhook", "--url", "https://hooks.example"}, "GROUND_CONTROL_WEBHOOK_SECRET environment variable is required"},
		{"webhook event type", []string{"create", "webhook", "--url", "https://hooks.example", "--event-type", "GroupChanged,"}, "--event-type must not be empty"},
		{"outbox state", []string{"get", "outbox-events", "--state", "done"}, "--state must be pending or failed"},
		{"outbox limit", []string{"get", "outbox-events", "--limit", "1001"}, "--limit must be between 1 and 1000"},
		{"outbox zero limit", []string{"get", "outbox-events", "--limit", "0"}, "--limit must be between 1 and 1000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"--server", server.URL, "--token", "session-token"}, tt.args...)
			_, err := execute(t, args...)
			require.ErrorContains(t, err, tt.want)
		})
	}
	require.Empty(t, requests())
}
