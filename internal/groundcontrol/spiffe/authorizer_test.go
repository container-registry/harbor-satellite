//go:build !nospiffe

package spiffe

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/stretchr/testify/require"
)

var (
	ourTD   = spiffeid.RequireTrustDomainFromString("harbor-satellite.test")
	otherTD = spiffeid.RequireTrustDomainFromString("other.test")
)

func sid(td spiffeid.TrustDomain, path string) spiffeid.ID { return spiffeid.RequireFromPath(td, path) }

func TestSatelliteAuthorizer(t *testing.T) {
	a := NewSatelliteAuthorizer(ourTD)
	tests := []struct {
		name string
		id   spiffeid.ID
		err  string
	}{
		{name: "full satellite path", id: sid(ourTD, "/satellite/region/eu/edge-01")},
		{name: "short satellite path", id: sid(ourTD, "/satellite/edge-01")},
		{name: "bare satellite", id: sid(ourTD, "/satellite")},
		{name: "ground control", id: sid(ourTD, "/gc/main"), err: "does not match satellite pattern"},
		{name: "prefix without separator", id: sid(ourTD, "/satellites/edge-01"), err: "does not match satellite pattern"},
		{name: "agent", id: sid(ourTD, "/agent/satellite"), err: "does not match satellite pattern"},
		{name: "other trust domain", id: sid(otherTD, "/satellite/region/eu/edge-01"), err: "unexpected trust domain"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, err := range []error{a.Authorize(tt.id, nil), a.AuthorizeID()(tt.id, nil)} {
				if tt.err == "" {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, tt.err)
				}
			}
		})
	}
}

func TestRegionAuthorizer(t *testing.T) {
	a := NewRegionAuthorizer(ourTD, []string{"eu", "us-west"})
	tests := []struct {
		name string
		id   spiffeid.ID
		err  string
	}{
		{name: "allowed region", id: sid(ourTD, "/satellite/region/eu/edge-01")},
		{name: "second allowed region", id: sid(ourTD, "/satellite/region/us-west/edge-02")},
		{name: "other region", id: sid(ourTD, "/satellite/region/ap/edge-03"), err: `region "ap" is not authorized`},
		{name: "no region in path", id: sid(ourTD, "/satellite/edge-01"), err: "does not contain region"},
		{name: "other trust domain", id: sid(otherTD, "/satellite/region/eu/edge-01"), err: "unexpected trust domain"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain := [][]*x509.Certificate{{{}}}
			for _, err := range []error{a.Authorize(tt.id, nil), a.AuthorizeID()(tt.id, chain)} {
				if tt.err == "" {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, tt.err)
				}
			}
		})
	}
}

func TestPatternAuthorizer(t *testing.T) {
	_, err := NewPatternAuthorizer(ourTD, "([")
	require.ErrorContains(t, err, "compile pattern")

	a, err := NewPatternAuthorizer(ourTD, `^/satellite/region/[a-z-]+/edge-\d+$`)
	require.NoError(t, err)
	require.NoError(t, a.Authorize(sid(ourTD, "/satellite/region/eu/edge-7"), nil))
	require.NoError(t, a.AuthorizeID()(sid(ourTD, "/satellite/region/eu/edge-7"), nil))
	require.ErrorContains(t, a.Authorize(sid(ourTD, "/satellite/region/eu/edge-7/x"), nil), "does not match pattern")
	require.ErrorContains(t, a.AuthorizeID()(sid(otherTD, "/satellite/region/eu/edge-7"), nil), "unexpected trust domain")
}

func TestExtractSatelliteNameAndRegion(t *testing.T) {
	tests := []struct {
		path      string
		name      string
		nameErr   bool
		region    string
		regionErr bool
	}{
		{path: "/satellite/region/eu/edge-01", name: "edge-01", region: "eu"},
		{path: "/satellite/edge-01", name: "edge-01", regionErr: true},
		{path: "/satellite", name: "default", regionErr: true},
		{path: "/gc/main", nameErr: true, regionErr: true},
		{path: "/satellites/edge-01", nameErr: true, regionErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			id := sid(ourTD, tt.path)
			name, err := ExtractSatelliteNameFromSPIFFEID(id)
			if tt.nameErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.name, name)
			}
			region, err := ExtractRegionFromSPIFFEID(id)
			if tt.regionErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.region, region)
			}
		})
	}
}

func TestBuildSPIFFEIDs(t *testing.T) {
	id, err := BuildSatelliteSPIFFEID(ourTD, "eu", "edge-01")
	require.NoError(t, err)
	require.Equal(t, "spiffe://harbor-satellite.test/satellite/region/eu/edge-01", id.String())
	name, err := ExtractSatelliteNameFromSPIFFEID(id)
	require.NoError(t, err)
	require.Equal(t, "edge-01", name, "a built ID parses back to its name")

	_, err = BuildSatelliteSPIFFEID(ourTD, "EU", "edge 01")
	require.Error(t, err, "SPIFFE paths allow neither upper case nor spaces")

	gc, err := BuildGroundControlSPIFFEID(ourTD)
	require.NoError(t, err)
	require.Equal(t, "spiffe://harbor-satellite.test/gc/main", gc.String())
}

func certWithURI(t *testing.T, raw string) *x509.Certificate {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return &x509.Certificate{URIs: []*url.URL{u}}
}

func TestExtractSPIFFEIDFromRequest(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	_, err := ExtractSPIFFEIDFromRequest(r)
	require.ErrorContains(t, err, "no TLS connection")

	r.TLS = &tls.ConnectionState{}
	_, err = ExtractSPIFFEIDFromRequest(r)
	require.ErrorContains(t, err, "no peer certificates")

	r.TLS.PeerCertificates = []*x509.Certificate{{}}
	_, err = ExtractSPIFFEIDFromRequest(r)
	require.ErrorContains(t, err, "no URI SANs")

	r.TLS.PeerCertificates = []*x509.Certificate{certWithURI(t, "https://example.test/")}
	_, err = ExtractSPIFFEIDFromRequest(r)
	require.ErrorContains(t, err, "no SPIFFE ID found")

	r.TLS.PeerCertificates = []*x509.Certificate{certWithURI(t, "spiffe://harbor-satellite.test/satellite/region/eu/edge-01")}
	id, err := ExtractSPIFFEIDFromRequest(r)
	require.NoError(t, err)
	require.Equal(t, sid(ourTD, "/satellite/region/eu/edge-01"), id)
}
