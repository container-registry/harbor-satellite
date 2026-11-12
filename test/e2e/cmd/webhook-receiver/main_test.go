package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/webhook"
	"github.com/stretchr/testify/require"
)

// The receiver must accept exactly what GC's signer produces, or the E2E
// would pass on a broken signature or fail on a correct one.
func TestHookVerifiesGroundControlSignature(t *testing.T) {
	secret := []byte("e2e-webhook-secret-of-at-least-32-bytes")
	r := &receiver{secret: secret}
	body := `{"id":"42","type":"io.harborsatellite.groundcontrol.GroupChanged","subject":"group/7"}`
	now := time.Unix(1_700_000_000, 0)

	for name, tc := range map[string]struct {
		secret []byte
		valid  bool
	}{
		"GC signature": {secret, true},
		"other secret": {[]byte("another-secret-of-at-least-32-bytes!!"), false},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/hook", strings.NewReader(body))
			req.Header.Set(webhook.TimestampHeader, strconv.FormatInt(now.Unix(), 10))
			req.Header.Set(webhook.SignatureHeader, webhook.Sign(tc.secret, now, []byte(body)))
			req.Header.Set("Idempotency-Key", "42")
			rec := httptest.NewRecorder()
			r.hook(rec, req)
			require.Equal(t, http.StatusNoContent, rec.Code)
			last := r.seen[len(r.seen)-1]
			require.Equal(t, tc.valid, last.SignatureValid)
			require.Equal(t, "42", last.ID)
			require.Equal(t, "42", last.IdempotencyKey)
		})
	}
}
