package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingLimits = "one shared per-IP limiter covers ZTR, login and every satellite beat"

func TestParseRate(t *testing.T) {
	for in, want := range map[string]Rate{
		"10/1m":  {Requests: 10, Window: time.Minute},
		"3/1s":   {Requests: 3, Window: time.Second},
		"100/1h": {Requests: 100, Window: time.Hour},
	} {
		got, err := ParseRate(in)
		require.NoError(t, err, in)
		require.Equal(t, want, got, in)
	}
	for _, in := range []string{"", "10", "/1m", "0/1m", "-1/1m", "x/1m", "10/", "10/0s", "10/-1s", "10/abc"} {
		_, err := ParseRate(in)
		require.Error(t, err, "%q", in)
	}
}

// appErrorBody is the server's AppError for 429; middleware cannot import
// the generated type.
const appErrorBody = `{"code":429,"message":"Too Many Requests"}`

func TestLimitBy_KeysAndAnswersAppError(t *testing.T) {
	pending.Plan(t, "4.6", pendingLimits)
	l := NewLimiter[string](Rate{Requests: 2, Window: time.Minute})
	t.Cleanup(l.Stop)
	handler := LimitBy(l, func(r *http.Request) (string, bool) {
		key := r.Header.Get("X-Key")
		return key, key != ""
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	send := func(key string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/satellites/sync", nil)
		if key != "" {
			req.Header.Set("X-Key", key)
		}
		handler.ServeHTTP(rec, req)
		return rec
	}

	for range 2 {
		require.Equal(t, http.StatusNoContent, send("edge-01").Code)
	}
	limited := send("edge-01")
	require.Equal(t, http.StatusTooManyRequests, limited.Code)
	require.Equal(t, "application/json", limited.Header().Get("Content-Type"))
	require.JSONEq(t, appErrorBody, limited.Body.String())
	require.Equal(t, http.StatusNoContent, send("edge-02").Code, "every key has its own budget")
	for range 3 {
		require.Equal(t, http.StatusNoContent, send("").Code, "a request without a key is not limited")
	}
}

func TestRateLimitMiddleware_WritesAppError(t *testing.T) {
	pending.Plan(t, "4.6", "the legacy limiter answers 429 with {\"error\": ...}, not the AppError of every other error")
	rl := NewRateLimiter(1, time.Minute)
	t.Cleanup(rl.Stop)
	handler := RateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	send := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/satellites/ztr", nil))
		return rec
	}

	require.Equal(t, http.StatusNoContent, send().Code)
	limited := send()
	require.Equal(t, http.StatusTooManyRequests, limited.Code)
	require.JSONEq(t, appErrorBody, limited.Body.String())
}
