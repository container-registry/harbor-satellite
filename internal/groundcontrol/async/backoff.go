package async

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// Backoff is the delay before attempt (1-based) is retried: base doubled
// per earlier attempt, capped at maxDelay.
func Backoff(attempt int, base, maxDelay time.Duration) time.Duration {
	d := base
	for i := 1; i < attempt; i++ {
		if d >= maxDelay/2 {
			return maxDelay
		}
		d *= 2
	}
	return min(d, maxDelay)
}

func digestOf(doc []byte) fleet.Digest {
	sum := sha256.Sum256(doc)
	return fleet.Digest("sha256:" + hex.EncodeToString(sum[:]))
}
