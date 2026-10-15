package lane

import "net/http"

// ThrottledTransport caps the bytes base moves per second, request and
// response bodies together, across every request it carries (plan item
// 6.2, replication_bandwidth_bytes_per_sec). It may burst one second's
// worth. A registry store applies it for
// store.RegistryOptions.BandwidthBytesPerSec, inside oras's retry
// transport, so retries spend the budget too. A bytesPerSec of zero or
// less means no cap and returns base.
func ThrottledTransport(base http.RoundTripper, bytesPerSec int64) http.RoundTripper {
	// 6.2 skeleton: no cap yet.
	return base
}
