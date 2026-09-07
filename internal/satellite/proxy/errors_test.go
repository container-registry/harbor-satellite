package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	proxy "github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/stretchr/testify/require"
)

func TestDistributionErrorCodesMatchSpecification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code   proxy.ErrorCode
		status int
	}{
		{proxy.ErrorCodeBlobUnknown, http.StatusNotFound},
		{proxy.ErrorCodeBlobUploadInvalid, http.StatusBadRequest},
		{proxy.ErrorCodeBlobUploadUnknown, http.StatusNotFound},
		{proxy.ErrorCodeDigestInvalid, http.StatusBadRequest},
		{proxy.ErrorCodeManifestBlobUnknown, http.StatusBadRequest},
		{proxy.ErrorCodeManifestInvalid, http.StatusBadRequest},
		{proxy.ErrorCodeManifestUnknown, http.StatusNotFound},
		{proxy.ErrorCodeNameInvalid, http.StatusBadRequest},
		{proxy.ErrorCodeNameUnknown, http.StatusNotFound},
		{proxy.ErrorCodeSizeInvalid, http.StatusBadRequest},
		{proxy.ErrorCodeUnauthorized, http.StatusUnauthorized},
		{proxy.ErrorCodeDenied, http.StatusForbidden},
		{proxy.ErrorCodeUnsupported, http.StatusMethodNotAllowed},
		{proxy.ErrorCodeTooManyRequests, http.StatusTooManyRequests},
	}

	seen := make(map[proxy.ErrorCode]struct{}, len(tests))
	for _, test := range tests {
		require.True(t, test.code.Valid())
		require.NotEmpty(t, test.code.Message())
		require.Equal(t, test.status, test.code.HTTPStatus())
		var distributionError *proxy.DistributionError
		require.ErrorAs(t, proxy.NewError(test.code, "", nil), &distributionError)
		require.Equal(t, test.status, distributionError.HTTPStatus())
		require.Equal(t, test.code.Message(), distributionError.Message)
		_, duplicate := seen[test.code]
		require.False(t, duplicate)
		seen[test.code] = struct{}{}
	}
	require.Len(t, seen, 14)
}

func TestNewErrorRejectsCustomWireCodes(t *testing.T) {
	t.Parallel()

	err := proxy.NewError(proxy.ErrorCode("POLICY_REJECTED"), "policy rejected request", nil)
	var distributionError *proxy.DistributionError
	require.NotErrorAs(t, err, &distributionError)
	require.Contains(t, err.Error(), "invalid OCI Distribution error code")
}

func TestUnsupportedRouteOverridesConventionalUnsupportedStatus(t *testing.T) {
	t.Parallel()

	response := serveRequest(t, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusMethodNotAllowed, proxy.ErrorCodeUnsupported.HTTPStatus())
	require.Equal(t, http.StatusNotFound, response.Code)
	require.Equal(t, proxy.ErrorCodeUnsupported, decodeError(t, response).Errors[0].Code)
}
