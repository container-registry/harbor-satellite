package webhook_test

import (
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/webhook"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

// The golden value is
// printf '1760529600.{"specversion":"1.0"}' | openssl dgst -sha256 -hmac 'whsec-0123456789abcdef0123456789abcdef'.
func TestSign_HMACSHA256OverTimestampAndBody(t *testing.T) {
	pending.Plan(t, "9.1", pendingWebhooks)
	got := webhook.Sign([]byte("whsec-0123456789abcdef0123456789abcdef"), time.Unix(1760529600, 0), []byte(`{"specversion":"1.0"}`))
	require.Equal(t, "sha256=ba3f376c274b41a30c86bfe0712a5e98b2d739859bcbdc81e55c205b2c7d12fc", got)
}
