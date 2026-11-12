package admission_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/admission"
	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/shared/logger/logtest"
	"github.com/container-registry/harbor-satellite/internal/shared/policybundle"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

// source is a BundleSource the test can switch between requests.
type source struct {
	mu     sync.Mutex
	bundle policybundle.Bundle
	mode   policybundle.Mode
	ok     bool
}

func (s *source) Current() (policybundle.Bundle, policybundle.Mode, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bundle, s.mode, s.ok
}

func (s *source) set(b policybundle.Bundle, mode policybundle.Mode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bundle, s.mode, s.ok = b, mode, true
}

type served struct {
	mu    sync.Mutex
	repos []string
}

// handler is the proxy with the admission middleware in front of a
// terminal handler that records what it served.
func handler(src admission.BundleSource) (http.Handler, *served) {
	s := &served{}
	terminal := func(r *proxy.Request) error {
		s.mu.Lock()
		s.repos = append(s.repos, r.Repository)
		s.mu.Unlock()
		return r.Write(http.StatusOK, []byte("{}"))
	}
	return proxy.New(terminal).Wrap(admission.Middleware(src)).Handler(), s
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

const blockedManifest = "/v2/library/blocked/manifests/latest"

func TestMiddleware_ModeOffAllowsAll(t *testing.T) {
	pending.Plan(t, "7.5", "the admission middleware is a stub")
	src := &source{}
	h, s := handler(src)
	require.Equal(t, http.StatusOK, get(t, h, blockedManifest).Code, "no bundle")

	src.set(bundle(blockRepo), policybundle.ModeOff)
	require.Equal(t, http.StatusOK, get(t, h, blockedManifest).Code, "mode off")
	require.Equal(t, []string{"library/blocked", "library/blocked"}, s.repos)
}

func TestMiddleware_AuditAllowsAndLogs(t *testing.T) {
	pending.Plan(t, "7.5", "the admission middleware is a stub")
	ctx, _, logs := logtest.Capture(t)
	src := &source{}
	src.set(bundle(allowAll, blockRepo), policybundle.ModeAudit)
	h, s := handler(src)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, blockedManifest, nil).WithContext(ctx))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []string{"library/blocked"}, s.repos)
	require.Contains(t, logs.String(), "library/blocked")
	require.Contains(t, logs.String(), "deny")
}

func TestMiddleware_EnforceDeniesWithOCIDenied(t *testing.T) {
	pending.Plan(t, "7.5", "the admission middleware is a stub")
	src := &source{}
	src.set(bundle(allowAll, blockRepo), policybundle.ModeEnforce)
	h, s := handler(src)

	rec := get(t, h, blockedManifest)

	require.Equal(t, http.StatusForbidden, rec.Code)
	var body struct {
		Errors []struct {
			Code proxy.ErrorCode `json:"code"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	require.Len(t, body.Errors, 1)
	require.Equal(t, proxy.ErrorCodeDenied, body.Errors[0].Code)
	require.Empty(t, s.repos, "a denied request never reaches the store")
	require.Equal(t, http.StatusOK, get(t, h, "/v2/library/alpine/manifests/latest").Code)

	src.set(policybundle.Bundle{ModelVersion: "edge-admission/v99", Policies: [][]string{allowAll}}, policybundle.ModeEnforce)
	require.Equal(t, http.StatusForbidden, get(t, h, "/v2/library/alpine/manifests/latest").Code, "an unknown model denies everything")
}

func TestMiddleware_ReloadsWhenBundleDigestChanges(t *testing.T) {
	pending.Plan(t, "7.5", "the admission middleware is a stub")
	src := &source{}
	src.set(bundle(allowAll), policybundle.ModeEnforce)
	h, _ := handler(src)
	require.Equal(t, http.StatusOK, get(t, h, blockedManifest).Code)

	src.set(bundle(allowAll, blockRepo), policybundle.ModeEnforce)
	require.Equal(t, http.StatusForbidden, get(t, h, blockedManifest).Code, "the cached allow went with the old bundle")

	src.set(bundle(blockRepo, allowAll), policybundle.ModeEnforce)
	require.Equal(t, http.StatusForbidden, get(t, h, blockedManifest).Code, "the same lines in another order")
}
