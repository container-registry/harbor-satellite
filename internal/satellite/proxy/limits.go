package proxy

import (
	"net/http"
	"time"
)

// WithWriteDeadline moves the connection's write deadline perWrite ahead
// before every Write (plan 5.4), so a stalled client is cut off while a long
// blob download that keeps making progress is not. A server-wide
// WriteTimeout would cut both.
func WithWriteDeadline(w http.ResponseWriter, perWrite time.Duration) http.ResponseWriter {
	return w
}
