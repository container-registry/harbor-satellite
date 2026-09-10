package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	proxy "github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/stretchr/testify/require"
)

func TestRepositoryValidation(t *testing.T) {
	t.Parallel()

	valid := []string{"a", "team/app", "a.b/c_d/e__f/g---h", "0/1a"}
	for _, repository := range valid {
		request := httptest.NewRequest(
			http.MethodGet,
			"/v2/"+repository+"/manifests/latest",
			nil,
		)
		response := serveRequest(t, request)
		require.Equal(t, http.StatusOK, response.Code, repository)
	}

	invalid := []string{"A", "a..b", "a___b", "a.-b", "a_"}
	for _, repository := range invalid {
		request := httptest.NewRequest(
			http.MethodGet,
			"/v2/"+repository+"/manifests/latest",
			nil,
		)
		requireDistributionResponse(
			t,
			request,
			proxy.ErrorCodeNameInvalid,
			http.StatusBadRequest,
		)
	}
}

func TestRepositoryHostAndNameCompatibilityLength(t *testing.T) {
	t.Parallel()

	const registryHost = "registry.example.org:5000"
	require.Len(t, registryHost, 25)

	for _, test := range []struct {
		name       string
		host       string
		repository string
		status     int
	}{
		{"255 characters", registryHost, strings.Repeat("a", 229), http.StatusOK},
		{"256 characters", registryHost, strings.Repeat("a", 230), http.StatusBadRequest},
		{"missing host", "", "team/app", http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(
				http.MethodGet,
				"/v2/"+test.repository+"/manifests/latest",
				nil,
			)
			request.Host = test.host
			response := serveRequest(t, request)
			require.Equal(t, test.status, response.Code)
			if test.status != http.StatusOK {
				require.Equal(
					t,
					proxy.ErrorCodeNameInvalid,
					decodeError(t, response).Errors[0].Code,
				)
			}
		})
	}
}

func TestTagValidation(t *testing.T) {
	t.Parallel()

	for _, tag := range []string{"Release_2026.08-rc1", "_staging"} {
		response := serveRequest(t, httptest.NewRequest(
			http.MethodGet,
			"/v2/team/app/manifests/"+tag,
			nil,
		))
		require.Equal(t, http.StatusOK, response.Code, tag)
	}

	for _, tag := range []string{".latest", "bad:tag", strings.Repeat("a", 129)} {
		requireDistributionResponse(
			t,
			httptest.NewRequest(
				http.MethodGet,
				"/v2/team/app/manifests/"+tag,
				nil,
			),
			proxy.ErrorCodeManifestInvalid,
			http.StatusBadRequest,
		)
	}
}

func TestCanonicalPathValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		rawPath string
	}{
		{"encoded separator", "/v2/team/app/manifests/latest", "/v2/team%2fapp/manifests/latest"},
		{"double separator", "/v2/team//app/manifests/latest", ""},
		{"traversal", "/v2/team/../app/manifests/latest", ""},
		{"backslash", "/v2/team\\app/manifests/latest", ""},
		{"space re-encoded by URL", "/v2/team/app/blobs/uploads/with space", ""},
		{"control byte", "/v2/team/\x1fapp/manifests/latest", ""},
		{"non ASCII", "/v2/team/\u00e9app/manifests/latest", ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(
				http.MethodGet,
				"/v2/team/app/manifests/latest",
				nil,
			)
			request.URL.Path = test.path
			request.URL.RawPath = test.rawPath
			requireDistributionResponse(
				t,
				request,
				proxy.ErrorCodeNameInvalid,
				http.StatusBadRequest,
			)
		})
	}
}

func TestVisibleASCIIUploadIDIsAccepted(t *testing.T) {
	t.Parallel()

	response := serveRequest(t, httptest.NewRequest(
		http.MethodGet,
		"/v2/team/app/blobs/uploads/01UPLOAD-._~",
		nil,
	))
	require.Equal(t, http.StatusOK, response.Code)
}
