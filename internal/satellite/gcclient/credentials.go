package gcclient

import "net/http"

// Credentials authenticates one request to Ground Control.
type Credentials interface {
	Authorize(req *http.Request)
}

var (
	_ Credentials = RobotBasic(nil)
	_ Credentials = NoCredentials{}
)

// RobotBasic sends the Harbor robot account as Basic auth. It is called per
// request so a refreshed secret applies without a new client.
type RobotBasic func() (user, pass string)

func (c RobotBasic) Authorize(req *http.Request) {
	req.SetBasicAuth(c())
}

// NoCredentials is for token ZTR, whose token travels in the body, and for
// SPIFFE satellites, whose HTTPClient presents the SVID.
type NoCredentials struct{}

func (NoCredentials) Authorize(*http.Request) {}
