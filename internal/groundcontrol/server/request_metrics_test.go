package server

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type observed struct {
	route, method string
	code          int
}

type fakeObserver struct {
	mu   sync.Mutex
	seen []observed
}

func (f *fakeObserver) ObserveHTTP(route, method string, code int, _ time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, observed{route, method, code})
}

// Requests are labelled by route template, so path values do not reach
// the label set; the security middleware's answers and unmatched paths
// are counted too.
func TestRequestMetrics_LabelByRouteTemplate(t *testing.T) {
	obs := &fakeObserver{}
	s := &Server{requests: obs}
	handler := s.RegisterRoutes()

	for _, r := range []struct{ method, path string }{
		{http.MethodGet, "/ping"},
		{http.MethodGet, "/api/groups/edge-group"},
		{http.MethodGet, "/no/such/route"},
	} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(r.method, r.path, nil))
	}

	require.Equal(t, []observed{
		{"/ping", http.MethodGet, http.StatusOK},
		{"/api/groups/{group}", http.MethodGet, http.StatusUnauthorized},
		{unmatchedRoute, http.MethodGet, http.StatusNotFound},
	}, obs.seen)
}
