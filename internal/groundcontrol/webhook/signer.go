package webhook

import "time"

const (
	// SignatureHeader is "sha256=<hex>" of HMAC-SHA256 over the Unix
	// timestamp, ".", and the body (ADR-0011 convention).
	SignatureHeader = "X-Ground-Control-Signature-256"
	TimestampHeader = "X-Ground-Control-Timestamp"
)

func Sign(secret []byte, timestamp time.Time, body []byte) string { return "" }
