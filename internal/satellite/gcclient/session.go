package gcclient

import (
	"net/http"
	"time"
)

var _ Credentials = SessionBearer(nil)

// Session is the Ground Control session a satellite authenticates with
// (plan item 4.1, ADR-0016); state maps it into pkg/config.GCSession.
type Session struct {
	Token     string
	ExpiresAt time.Time
}

// SessionBearer reads the token per request, so a renewal applies without a new client.
type SessionBearer func() string

func (c SessionBearer) Authorize(req *http.Request) { req.Header.Set("Authorization", "Bearer "+c()) }
