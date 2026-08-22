package proxy_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	proxy "github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/stretchr/testify/require"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type errorEnvelope struct {
	Errors []struct {
		Code    proxy.ErrorCode `json:"code"`
		Message string          `json:"message"`
	} `json:"errors"`
}

func terminalLayer(handler proxy.Handler) proxy.Layer {
	return proxy.LayerFunc(func(proxy.Handler) proxy.Handler { return handler })
}

func requireStack(t *testing.T, layers ...proxy.Layer) *proxy.Stack {
	t.Helper()
	stack, err := proxy.New(layers...)
	require.NoError(t, err)
	return stack
}

func serveRequest(
	t *testing.T,
	request *http.Request,
	layers ...proxy.Layer,
) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	requireStack(t, layers...).Handler().ServeHTTP(response, request)
	return response
}

func decodeError(t *testing.T, response *httptest.ResponseRecorder) errorEnvelope {
	t.Helper()
	var envelope errorEnvelope
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.Len(t, envelope.Errors, 1)
	return envelope
}

func requireDistributionResponse(
	t *testing.T,
	request *http.Request,
	code proxy.ErrorCode,
	status int,
) {
	t.Helper()
	response := serveRequest(t, request)
	require.Equal(t, status, response.Code)
	require.Equal(t, code, decodeError(t, response).Errors[0].Code)
}
