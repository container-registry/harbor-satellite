package proxy

import (
	"net"
	"net/http"
	"time"

	"golang.org/x/net/netutil"
)

// WithWriteDeadline moves the connection's write deadline perWrite ahead
// before every Write (plan 5.4), so a stalled client is cut off while a long
// blob download that keeps making progress is not. A server-wide
// WriteTimeout would cut both. The wrapper deliberately hides ReadFrom: one
// sendfile call for a whole blob would run under a single deadline.
func WithWriteDeadline(w http.ResponseWriter, perWrite time.Duration) http.ResponseWriter {
	if perWrite <= 0 {
		return w
	}
	return &deadlineWriter{ResponseWriter: w, controller: http.NewResponseController(w), perWrite: perWrite}
}

type deadlineWriter struct {
	http.ResponseWriter
	controller *http.ResponseController
	perWrite   time.Duration
}

func (w *deadlineWriter) extend() {
	// ErrNotSupported leaves the writer without a deadline, as before 5.4.
	_ = w.controller.SetWriteDeadline(time.Now().Add(w.perWrite)) //nolint:errcheck // see above
}

func (w *deadlineWriter) WriteHeader(status int) {
	w.extend()
	w.ResponseWriter.WriteHeader(status)
}

func (w *deadlineWriter) Write(p []byte) (int, error) {
	w.extend()
	return w.ResponseWriter.Write(p)
}

func (w *deadlineWriter) Flush() {
	w.extend()
	// http.Flusher reports no error; a failed flush shows on the next Write.
	_ = w.controller.Flush()
}

func (w *deadlineWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// WriteDeadlineHandler applies WithWriteDeadline to every request of next.
// It sets the deadline before the handler and again after it, so the
// server's final flush and a response without a body are bounded too, and
// a deadline left by an earlier request on a kept-alive connection is moved.
func WriteDeadlineHandler(next http.Handler, perWrite time.Duration) http.Handler {
	if perWrite <= 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limited, ok := WithWriteDeadline(w, perWrite).(*deadlineWriter)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		limited.extend()
		next.ServeHTTP(limited, r)
		limited.extend()
	})
}

// LimitListener caps concurrent connections; zero or less leaves l as is.
func LimitListener(l net.Listener, maxConnections int) net.Listener {
	if maxConnections <= 0 {
		return l
	}
	return netutil.LimitListener(l, maxConnections)
}
