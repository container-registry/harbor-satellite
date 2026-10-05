package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	proxy "github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	digest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func TestHTTPParserCreatesRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		target     string
		operation  proxy.Operation
		resource   proxy.Resource
		repository string
		reference  string
		digest     digest.Digest
		uploadID   string
		source     string
	}{
		{name: "check registry", method: http.MethodGet, target: "/v2/", operation: proxy.CheckRegistry, resource: proxy.ResourceRegistry},
		{name: "pull manifest by tag", method: http.MethodGet, target: "/v2/team/app/manifests/v1.2", operation: proxy.PullManifest, resource: proxy.ResourceManifest, repository: "team/app", reference: "v1.2"},
		{name: "check manifest by digest", method: http.MethodHead, target: "/v2/team/app/manifests/" + testDigest, operation: proxy.CheckManifest, resource: proxy.ResourceManifest, repository: "team/app", reference: testDigest, digest: testDigest},
		{name: "push manifest", method: http.MethodPut, target: "/v2/team/app/manifests/latest", operation: proxy.PushManifest, resource: proxy.ResourceManifest, repository: "team/app", reference: "latest"},
		{name: "delete manifest", method: http.MethodDelete, target: "/v2/team/app/manifests/" + testDigest, operation: proxy.DeleteManifest, resource: proxy.ResourceManifest, repository: "team/app", reference: testDigest, digest: testDigest},
		{name: "pull blob", method: http.MethodGet, target: "/v2/team/app/blobs/" + testDigest, operation: proxy.PullBlob, resource: proxy.ResourceBlob, repository: "team/app", digest: testDigest},
		{name: "check blob", method: http.MethodHead, target: "/v2/team/app/blobs/" + testDigest, operation: proxy.CheckBlob, resource: proxy.ResourceBlob, repository: "team/app", digest: testDigest},
		{name: "delete blob", method: http.MethodDelete, target: "/v2/team/app/blobs/" + testDigest, operation: proxy.DeleteBlob, resource: proxy.ResourceBlob, repository: "team/app", digest: testDigest},
		{name: "start upload", method: http.MethodPost, target: "/v2/team/app/blobs/uploads/", operation: proxy.StartBlobUpload, resource: proxy.ResourceBlobUpload, repository: "team/app"},
		{name: "push blob monolithically", method: http.MethodPost, target: "/v2/team/app/blobs/uploads/?digest=" + testDigest, operation: proxy.PushBlob, resource: proxy.ResourceBlob, repository: "team/app", digest: testDigest},
		{name: "mount blob", method: http.MethodPost, target: "/v2/team/app/blobs/uploads/?mount=" + testDigest + "&from=base/images", operation: proxy.MountBlob, resource: proxy.ResourceBlob, repository: "team/app", digest: testDigest, source: "base/images"},
		{name: "check upload", method: http.MethodGet, target: "/v2/team/app/blobs/uploads/01UPLOAD", operation: proxy.CheckBlobUpload, resource: proxy.ResourceBlobUpload, repository: "team/app", uploadID: "01UPLOAD"},
		{name: "update upload", method: http.MethodPatch, target: "/v2/team/app/blobs/uploads/01UPLOAD", operation: proxy.UpdateBlobUpload, resource: proxy.ResourceBlobUpload, repository: "team/app", uploadID: "01UPLOAD"},
		{name: "complete upload", method: http.MethodPut, target: "/v2/team/app/blobs/uploads/01UPLOAD?digest=" + testDigest, operation: proxy.CompleteBlobUpload, resource: proxy.ResourceBlobUpload, repository: "team/app", digest: testDigest, uploadID: "01UPLOAD"},
		{name: "list tags", method: http.MethodGet, target: "/v2/team/app/tags/list?n=10&last=v1.0", operation: proxy.ListTags, resource: proxy.ResourceTags, repository: "team/app"},
		{name: "list referrers", method: http.MethodGet, target: "/v2/team/app/referrers/" + testDigest + "?artifactType=application%2Fvnd.example.sbom.v1", operation: proxy.ListReferrers, resource: proxy.ResourceReferrers, repository: "team/app", digest: testDigest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(test.method, test.target, nil)
			response := httptest.NewRecorder()
			var requestState *proxy.Request
			handler := proxy.New(proxy.Process(func(current *proxy.Request) error {
				requestState = current
				return nil
			})).Handler()
			handler.ServeHTTP(response, request)

			require.NotNil(t, requestState)
			require.Same(t, request, requestState.HTTPRequest())
			require.Equal(t, request.Context(), requestState.Context())
			require.Equal(t, test.operation, requestState.Operation)
			require.Equal(t, proxy.Method(test.method), requestState.Method)
			require.Equal(t, test.resource, requestState.Resource())
			require.Equal(t, test.repository, requestState.Repository)
			require.Equal(t, test.reference, requestState.Reference)
			require.Equal(t, test.digest, requestState.Digest)
			require.Equal(t, test.uploadID, requestState.UploadID)
			require.Equal(t, test.source, requestState.SourceRepository)
		})
	}
}

func TestHTTPParserUsesFirstKnownQueryValue(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(
		http.MethodGet,
		"/v2/team/app/tags/list?n=0&n=20&last=v1&last=v2",
		nil,
	)
	var requestState *proxy.Request
	response := serveRequest(t, request, proxy.Process(func(current *proxy.Request) error {
		requestState = current
		return nil
	}))

	require.Equal(t, http.StatusOK, response.Code)
	require.NotNil(t, requestState)
	require.NotNil(t, requestState.Query.N)
	require.Equal(t, 0, *requestState.Query.N)
	require.NotNil(t, requestState.Query.Last)
	require.Equal(t, "v1", *requestState.Query.Last)
	require.Nil(t, requestState.Query.Digest)
}

func TestRequestQueryStoresValidatedParameters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		target string
		assert func(*testing.T, proxy.Query)
	}{
		{
			"tag pagination",
			http.MethodGet,
			"/v2/team/app/tags/list?n=10&last=v1",
			func(t *testing.T, query proxy.Query) {
				t.Helper()
				require.NotNil(t, query.N)
				require.Equal(t, 10, *query.N)
				require.NotNil(t, query.Last)
				require.Equal(t, "v1", *query.Last)
			},
		},
		{
			"blob mount",
			http.MethodPost,
			"/v2/team/app/blobs/uploads/?mount=" + testDigest + "&from=base/images",
			func(t *testing.T, query proxy.Query) {
				t.Helper()
				require.NotNil(t, query.Mount)
				require.Equal(t, testDigest, *query.Mount)
				require.NotNil(t, query.From)
				require.Equal(t, "base/images", *query.From)
			},
		},
		{
			"upload completion",
			http.MethodPut,
			"/v2/team/app/blobs/uploads/01UPLOAD?digest=" + testDigest,
			func(t *testing.T, query proxy.Query) {
				t.Helper()
				require.NotNil(t, query.Digest)
				require.Equal(t, testDigest, *query.Digest)
			},
		},
		{
			"artifact type",
			http.MethodGet,
			"/v2/team/app/referrers/" + testDigest +
				"?artifactType=application%2Fvnd.example.sbom.v1",
			func(t *testing.T, query proxy.Query) {
				t.Helper()
				require.NotNil(t, query.ArtifactType)
				require.Equal(t, "application/vnd.example.sbom.v1", *query.ArtifactType)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var requestState *proxy.Request
			response := serveRequest(
				t,
				httptest.NewRequest(test.method, test.target, nil),
				proxy.Process(func(current *proxy.Request) error {
					requestState = current
					return nil
				}),
			)
			require.Equal(t, http.StatusOK, response.Code)
			require.NotNil(t, requestState)

			test.assert(t, requestState.Query)
		})
	}
}

func TestRequestQueryLeavesAbsentParameterNil(t *testing.T) {
	t.Parallel()

	var requestState *proxy.Request
	response := serveRequest(
		t,
		httptest.NewRequest(http.MethodGet, "/v2/team/app/tags/list", nil),
		proxy.Process(func(current *proxy.Request) error {
			requestState = current
			return nil
		}),
	)
	require.Equal(t, http.StatusOK, response.Code)
	require.NotNil(t, requestState)

	require.Nil(t, requestState.Query.N)
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
		{"ping without trailing slash", http.MethodGet, "/v2", proxy.ErrorCodeUnsupported, http.StatusNotFound},
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
		{"malformed upload inspection query", http.MethodGet, "/v2/team/app/blobs/uploads/01UPLOAD?%zz", proxy.ErrorCodeBlobUploadInvalid, http.StatusBadRequest},
		{"malformed upload patch query", http.MethodPatch, "/v2/team/app/blobs/uploads/01UPLOAD?%zz", proxy.ErrorCodeBlobUploadInvalid, http.StatusBadRequest},
		{"malformed tags query", http.MethodGet, "/v2/team/app/tags/list?%zz", proxy.ErrorCodeUnsupported, http.StatusBadRequest},
		{"negative tag limit", http.MethodGet, "/v2/team/app/tags/list?n=-1", proxy.ErrorCodeUnsupported, http.StatusBadRequest},
		{"invalid last tag", http.MethodGet, "/v2/team/app/tags/list?last=.bad", proxy.ErrorCodeUnsupported, http.StatusBadRequest},
		{"malformed referrers query", http.MethodGet, "/v2/team/app/referrers/" + testDigest + "?%zz", proxy.ErrorCodeUnsupported, http.StatusBadRequest},
		{"invalid artifact type", http.MethodGet, "/v2/team/app/referrers/" + testDigest + "?artifactType=not-a-media-type", proxy.ErrorCodeUnsupported, http.StatusBadRequest},
		{"artifact type parameters", http.MethodGet, "/v2/team/app/referrers/" + testDigest + "?artifactType=application%2Fjson%3Bcharset%3Dutf-8", proxy.ErrorCodeUnsupported, http.StatusBadRequest},
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

func TestMethodNotAllowedIncludesAllowHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target string
		allow  string
	}{
		{"ping", "/v2/", "GET"},
		{"manifest", "/v2/team/app/manifests/latest", "GET, HEAD, PUT, DELETE"},
		{"blob", "/v2/team/app/blobs/" + testDigest, "GET, HEAD, DELETE"},
		{"upload start", "/v2/team/app/blobs/uploads/", "POST"},
		{"upload session", "/v2/team/app/blobs/uploads/01UPLOAD", "GET, PATCH, PUT"},
		{"tags", "/v2/team/app/tags/list", "GET"},
		{"referrers", "/v2/team/app/referrers/" + testDigest, "GET"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			response := serveRequest(t, httptest.NewRequest(http.MethodOptions, test.target, nil))
			require.Equal(t, http.StatusMethodNotAllowed, response.Code)
			require.Equal(t, test.allow, response.Header().Get("Allow"))
		})
	}
}

func TestOperationMetadata(t *testing.T) {
	t.Parallel()

	require.Equal(t, "pull_manifest", proxy.PullManifest.String())
	require.Equal(t, proxy.ResourceManifest, proxy.PullManifest.Resource())
	require.True(t, proxy.PullManifest.IsPull())
	require.Equal(t, "blob_upload", proxy.ResourceBlobUpload.String())
	require.Equal(t, "unknown", proxy.OperationUnknown.String())
	require.Equal(t, "GET", proxy.GET.String())
	require.Equal(t, "unknown", proxy.Unknown.String())
}

func TestRequestRetainsStandardHTTPObjects(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	response := httptest.NewRecorder()
	handler := proxy.New(proxy.Process(func(requestState *proxy.Request) error {
		require.Same(t, request, requestState.HTTPRequest())
		requestState.HTTPRequest().Header.Set("Authorization", "updated")
		requestState.ResponseHeader().Set("X-Satellite", "handler")
		return nil
	})).Handler()
	handler.ServeHTTP(response, request)

	require.Equal(t, "updated", request.Header.Get("Authorization"))
	require.Equal(t, "handler", response.Header().Get("X-Satellite"))
}
