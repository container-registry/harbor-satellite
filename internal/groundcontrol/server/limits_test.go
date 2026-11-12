package server

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingSplitLimits = "one shared per-IP limiter covers ZTR, login and every satellite beat"

// Ten satellites behind one NAT; the legacy limiter (10 per minute per IP)
// would answer 429 from the 11th request on. A minute window keeps the 40
// round trips inside it, so the test checks the keying, not the timing.
func TestFleetLimiter_TenSatellitesBehindOneIPGetNo429(t *testing.T) {
	pending.Plan(t, "4.6", pendingSplitLimits)
	splitLimits(t, "100/1m", "3/1m")
	h := newHotPathHarness(t)
	h.edgeSetup()
	tokens := map[string]string{}
	for i := range 10 {
		name := fmt.Sprintf("edge-%02d", i)
		_, tokens[name] = h.onboardWithSession(name)
	}

	for range 3 {
		for name, token := range tokens {
			h.mustDo(http.MethodPost, "/satellites/sync", heartbeatRequest(name), bearerAuth(token), http.StatusOK)
		}
	}
	for name, token := range tokens {
		require.Equal(t, http.StatusTooManyRequests, h.do(http.MethodPost, "/satellites/sync", heartbeatRequest(name), bearerAuth(token)).Status,
			"%s: the fleet limit is per satellite", name)
		break
	}
}

func TestAnonymousLimiter_ZTRLimitedPerClientIP(t *testing.T) {
	pending.Plan(t, "4.6", pendingSplitLimits)
	splitLimits(t, "2/1m", "3/1s")
	t.Setenv("AUDIT_TRUST_FORWARDED_HEADERS", "true")
	h := newGCHarness(t)
	ztrFrom := func(ip string) response {
		t.Helper()
		req := h.newRequest(http.MethodPost, "/satellites/ztr", ZTRRequest{Token: "unknown"}, nil)
		req.Header.Set("X-Forwarded-For", ip)
		r, err := send(req)
		require.NoError(t, err)
		return r
	}

	for range 2 {
		require.NotEqual(t, http.StatusTooManyRequests, ztrFrom("10.0.0.1").Status)
	}
	limited := ztrFrom("10.0.0.1")
	require.Equal(t, http.StatusTooManyRequests, limited.Status)
	var body AppError
	limited.decode(t, &body)
	require.Equal(t, AppError{Code: http.StatusTooManyRequests, Message: "Too Many Requests"}, body)
	require.NotEqual(t, http.StatusTooManyRequests, ztrFrom("10.0.0.2").Status, "another client IP has its own budget")
}
