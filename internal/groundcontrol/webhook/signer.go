package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"
)

const (
	// SignatureHeader is "sha256=<hex>" of HMAC-SHA256 over the Unix
	// timestamp, ".", and the body (ADR-0011 convention).
	SignatureHeader = "X-Ground-Control-Signature-256"
	TimestampHeader = "X-Ground-Control-Timestamp"
)

func Sign(secret []byte, timestamp time.Time, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(strconv.FormatInt(timestamp.Unix(), 10)))
	mac.Write([]byte{'.'})
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
