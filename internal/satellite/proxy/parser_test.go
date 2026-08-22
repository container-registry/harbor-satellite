package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	proxy "github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	digest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func TestHTTPParserCreatesContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		target     string
		operation  proxy.Operation
		access     proxy.Access
		repository string
		reference  string
		digest     digest.Digest
		uploadID   string
		source     string
	}{
		{"ping", http.MethodGet, "/v2/", proxy.Ping, proxy.ReadAccess, "", "", "", "", ""},
		{"manifest tag", http.MethodGet, "/v2/team/app/manifests/v1.2", proxy.Manifest, proxy.ReadAccess, "team/app", "v1.2", "", "", ""},
		{"manifest digest", http.MethodHead, "/v2/team/app/manifests/" + testDigest, proxy.Manifest, proxy.ReadAccess, "team/app", testDigest, testDigest, "", ""},
		{"publish manifest", http.MethodPut, "/v2/team/app/manifests/latest", proxy.Manifest, proxy.WriteAccess, "team/app", "latest", "", "", ""},
		{"delete manifest", http.MethodDelete, "/v2/team/app/manifests/" + testDigest, proxy.Manifest, proxy.DeleteAccess, "team/app", testDigest, testDigest, "", ""},
		{"pull blob", http.MethodGet, "/v2/team/app/blobs/" + testDigest, proxy.Blob, proxy.ReadAccess, "team/app", "", testDigest, "", ""},
		{"delete blob", http.MethodDelete, "/v2/team/app/blobs/" + testDigest, proxy.Blob, proxy.DeleteAccess, "team/app", "", testDigest, "", ""},
		{"start upload", http.MethodPost, "/v2/team/app/blobs/uploads/", proxy.BlobUpload, proxy.WriteAccess, "team/app", "", "", "", ""},
		{"monolithic upload", http.MethodPost, "/v2/team/app/blobs/uploads/?digest=" + testDigest, proxy.BlobUpload, proxy.WriteAccess, "team/app", "", testDigest, "", ""},
		{"mount blob", http.MethodPost, "/v2/team/app/blobs/uploads/?mount=" + testDigest + "&from=base/images", proxy.BlobMount, proxy.WriteAccess, "team/app", "", testDigest, "", "base/images"},
		{"inspect upload", http.MethodGet, "/v2/team/app/blobs/uploads/01UPLOAD", proxy.BlobUpload, proxy.ReadAccess, "team/app", "", "", "01UPLOAD", ""},
		{"update upload", http.MethodPatch, "/v2/team/app/blobs/uploads/01UPLOAD", proxy.BlobUpload, proxy.WriteAccess, "team/app", "", "", "01UPLOAD", ""},
		{"complete upload", http.MethodPut, "/v2/team/app/blobs/uploads/01UPLOAD?digest=" + testDigest, proxy.BlobUpload, proxy.WriteAccess, "team/app", "", testDigest, "01UPLOAD", ""},
		{"list tags", http.MethodGet, "/v2/team/app/tags/list?n=10", proxy.Tags, proxy.ReadAccess, "team/app", "", "", "", ""},
		{"list referrers", http.MethodGet, "/v2/team/app/referrers/" + testDigest, proxy.Referrers, proxy.ReadAccess, "team/app", "", testDigest, "", ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(test.method, test.target, nil)
			response := httptest.NewRecorder()
			var contract *proxy.Contract
			capture := terminalLayer(proxy.HandlerFunc(func(current *proxy.Contract) error {
				contract = current
				return nil
			}))
			requireStack(t, capture).Handler().ServeHTTP(response, request)

			require.NotNil(t, contract)
			require.Same(t, request, contract.Request)
			require.Same(t, response, contract.Response)
			require.Equal(t, request.Context(), contract.Context())
			require.Equal(t, test.operation, contract.Operation)
			require.Equal(t, test.access, contract.Access)
			require.Equal(t, test.repository, contract.Repository)
			require.Equal(t, test.reference, contract.Reference)
			require.Equal(t, test.digest, contract.Digest)
			require.Equal(t, test.uploadID, contract.UploadID)
			require.Equal(t, test.source, contract.SourceRepository)
		})
	}
}

func TestHTTPParserRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		target string
		code   proxy.ErrorCode
		status int
	}{
		{"unknown route", http.MethodGet, "/metrics", proxy.ErrorCodeUnsupported, http.StatusNotFound},
		{"vendor route", http.MethodGet, "/v2/team/app/search", proxy.ErrorCodeUnsupported, http.StatusNotFound},
		{"catalog extension", http.MethodGet, "/v2/_catalog", proxy.ErrorCodeUnsupported, http.StatusNotFound},
		{"wrong method", http.MethodPost, "/v2/team/app/manifests/latest", proxy.ErrorCodeUnsupported, http.StatusMethodNotAllowed},
		{"head tags", http.MethodHead, "/v2/team/app/tags/list", proxy.ErrorCodeUnsupported, http.StatusMethodNotAllowed},
		{"cancel upload", http.MethodDelete, "/v2/team/app/blobs/uploads/01UPLOAD", proxy.ErrorCodeUnsupported, http.StatusMethodNotAllowed},
		{"upload missing slash", http.MethodPost, "/v2/team/app/blobs/uploads", proxy.ErrorCodeDigestInvalid, http.StatusBadRequest},
		{"invalid blob digest", http.MethodGet, "/v2/team/app/blobs/sha256:abc", proxy.ErrorCodeDigestInvalid, http.StatusBadRequest},
		{"incomplete mount", http.MethodPost, "/v2/team/app/blobs/uploads/?mount=" + testDigest, proxy.ErrorCodeBlobUploadInvalid, http.StatusBadRequest},
		{"malformed mount", http.MethodPost, "/v2/team/app/blobs/uploads/?mount=%zz&from=base", proxy.ErrorCodeBlobUploadInvalid, http.StatusBadRequest},
		{"invalid mount source", http.MethodPost, "/v2/team/app/blobs/uploads/?mount=" + testDigest + "&from=Base", proxy.ErrorCodeNameInvalid, http.StatusBadRequest},
		{"missing completion digest", http.MethodPut, "/v2/team/app/blobs/uploads/01UPLOAD", proxy.ErrorCodeDigestInvalid, http.StatusBadRequest},
		{"duplicate completion digest", http.MethodPut, "/v2/team/app/blobs/uploads/01UPLOAD?digest=" + testDigest + "&digest=" + testDigest, proxy.ErrorCodeDigestInvalid, http.StatusBadRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(test.method, test.target, nil)
			if test.method == http.MethodHead {
				response := serveRequest(t, request)
				require.Equal(t, test.status, response.Code)
				require.Empty(t, response.Body.String())
				return
			}
			requireDistributionResponse(t, request, test.code, test.status)
		})
	}
}

func TestOperationMetadata(t *testing.T) {
	t.Parallel()

	require.Equal(t, "manifest", proxy.Manifest.String())
	require.Equal(t, "blob_upload", proxy.BlobUpload.String())
	require.Equal(t, "unknown", proxy.UnknownOperation.String())
	require.Equal(t, "write", proxy.WriteAccess.String())
}

func TestContractRetainsStandardHTTPObjects(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	response := httptest.NewRecorder()
	inspect := terminalLayer(proxy.HandlerFunc(func(contract *proxy.Contract) error {
		require.Same(t, request, contract.Request)
		require.Same(t, response, contract.Response)
		contract.Request.Header.Set("Authorization", "updated")
		contract.Response.Header().Set("X-Satellite", "layer")
		return nil
	}))
	requireStack(t, inspect).Handler().ServeHTTP(response, request)

	require.Equal(t, "updated", request.Header.Get("Authorization"))
	require.Equal(t, "layer", response.Header().Get("X-Satellite"))
}
