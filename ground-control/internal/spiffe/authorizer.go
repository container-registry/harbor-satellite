package spiffe

import (
	"crypto/x509"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
)

// SatelliteAuthorizer authorizes connections from satellites based on their SPIFFE ID.
type SatelliteAuthorizer struct {
	trustDomain spiffeid.TrustDomain
	pathPrefix  string
}

// NewSatelliteAuthorizer creates an authorizer that accepts satellites
// with SPIFFE IDs matching the pattern:
// spiffe://<trust-domain>/satellite/region/<region>/<name>
func NewSatelliteAuthorizer(trustDomain spiffeid.TrustDomain) *SatelliteAuthorizer {
	return &SatelliteAuthorizer{
		trustDomain: trustDomain,
		pathPrefix:  "/satellite/",
	}
}

// Authorize validates that the peer's SPIFFE ID belongs to a satellite.
func (a *SatelliteAuthorizer) Authorize(actual spiffeid.ID, _ *x509.Certificate) error {
	if actual.TrustDomain() != a.trustDomain {
		return fmt.Errorf("unexpected trust domain: got %s, want %s",
			actual.TrustDomain(), a.trustDomain)
	}

	// Accept exact /satellite path or any path starting with /satellite/
	path := actual.Path()
	if path != "/satellite" && !strings.HasPrefix(path, a.pathPrefix) {
		return fmt.Errorf("SPIFFE ID path %q does not match satellite pattern", path)
	}

	return nil
}

// AuthorizeID returns a tlsconfig.Authorizer for use with go-spiffe.
func (a *SatelliteAuthorizer) AuthorizeID() tlsconfig.Authorizer {
	return func(id spiffeid.ID, verifiedChains [][]*x509.Certificate) error {
		var cert *x509.Certificate
		if len(verifiedChains) > 0 && len(verifiedChains[0]) > 0 {
			cert = verifiedChains[0][0]
		}
		return a.Authorize(id, cert)
	}
}

// RegionAuthorizer authorizes satellites from specific regions only.
type RegionAuthorizer struct {
	trustDomain    spiffeid.TrustDomain
	allowedRegions map[string]bool
}

// NewRegionAuthorizer creates an authorizer that only accepts satellites
// from the specified regions.
func NewRegionAuthorizer(trustDomain spiffeid.TrustDomain, regions []string) *RegionAuthorizer {
	allowed := make(map[string]bool, len(regions))
	for _, r := range regions {
		allowed[r] = true
	}
	return &RegionAuthorizer{
		trustDomain:    trustDomain,
		allowedRegions: allowed,
	}
}

// Authorize validates the SPIFFE ID is from an allowed region.
func (a *RegionAuthorizer) Authorize(actual spiffeid.ID, _ *x509.Certificate) error {
	if actual.TrustDomain() != a.trustDomain {
		return fmt.Errorf("unexpected trust domain: got %s, want %s",
			actual.TrustDomain(), a.trustDomain)
	}

	region, err := ExtractRegionFromSPIFFEID(actual)
	if err != nil {
		return err
	}

	if !a.allowedRegions[region] {
		return fmt.Errorf("region %q is not authorized", region)
	}

	return nil
}

// AuthorizeID returns a tlsconfig.Authorizer for use with go-spiffe.
func (a *RegionAuthorizer) AuthorizeID() tlsconfig.Authorizer {
	return func(id spiffeid.ID, verifiedChains [][]*x509.Certificate) error {
		var cert *x509.Certificate
		if len(verifiedChains) > 0 && len(verifiedChains[0]) > 0 {
			cert = verifiedChains[0][0]
		}
		return a.Authorize(id, cert)
	}
}

// PatternAuthorizer authorizes SPIFFE IDs matching a regex pattern.
type PatternAuthorizer struct {
	trustDomain spiffeid.TrustDomain
	pattern     *regexp.Regexp
}

// NewPatternAuthorizer creates an authorizer that accepts SPIFFE IDs
// matching the given regex pattern on the path component.
func NewPatternAuthorizer(trustDomain spiffeid.TrustDomain, pattern string) (*PatternAuthorizer, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("compile pattern: %w", err)
	}
	return &PatternAuthorizer{
		trustDomain: trustDomain,
		pattern:     re,
	}, nil
}

// Authorize validates the SPIFFE ID path matches the pattern.
func (a *PatternAuthorizer) Authorize(actual spiffeid.ID, _ *x509.Certificate) error {
	if actual.TrustDomain() != a.trustDomain {
		return fmt.Errorf("unexpected trust domain: got %s, want %s",
			actual.TrustDomain(), a.trustDomain)
	}

	if !a.pattern.MatchString(actual.Path()) {
		return fmt.Errorf("SPIFFE ID path %q does not match pattern %s",
			actual.Path(), a.pattern.String())
	}

	return nil
}

// AuthorizeID returns a tlsconfig.Authorizer for use with go-spiffe.
func (a *PatternAuthorizer) AuthorizeID() tlsconfig.Authorizer {
	return func(id spiffeid.ID, verifiedChains [][]*x509.Certificate) error {
		var cert *x509.Certificate
		if len(verifiedChains) > 0 && len(verifiedChains[0]) > 0 {
			cert = verifiedChains[0][0]
		}
		return a.Authorize(id, cert)
	}
}

// ExtractSPIFFEIDFromRequest extracts the SPIFFE ID from an HTTP request's TLS connection.
func ExtractSPIFFEIDFromRequest(r *http.Request) (spiffeid.ID, error) {
	if r.TLS == nil {
		return spiffeid.ID{}, fmt.Errorf("no TLS connection")
	}

	if len(r.TLS.PeerCertificates) == 0 {
		return spiffeid.ID{}, fmt.Errorf("no peer certificates")
	}

	cert := r.TLS.PeerCertificates[0]
	return ExtractSPIFFEIDFromCert(cert)
}

// ExtractSPIFFEIDFromCert extracts the SPIFFE ID from an X.509 certificate.
func ExtractSPIFFEIDFromCert(cert *x509.Certificate) (spiffeid.ID, error) {
	if len(cert.URIs) == 0 {
		return spiffeid.ID{}, fmt.Errorf("certificate has no URI SANs")
	}

	for _, uri := range cert.URIs {
		if uri.Scheme == "spiffe" {
			return spiffeid.FromURI(uri)
		}
	}

	return spiffeid.ID{}, fmt.Errorf("no SPIFFE ID found in certificate URIs")
}

// ExtractSatelliteNameFromSPIFFEID extracts the satellite name from a SPIFFE ID.
// Supported formats:
// - spiffe://<trust-domain>/satellite (simple, name is "default")
// - spiffe://<trust-domain>/satellite/<name> (direct name)
// - spiffe://<trust-domain>/satellite/region/<region>/<name> (full path)
func ExtractSatelliteNameFromSPIFFEID(id spiffeid.ID) (string, error) {
	path := id.Path()

	// Handle exact /satellite path
	if path == "/satellite" {
		return "default", nil
	}

	if !strings.HasPrefix(path, "/satellite/") {
		return "", fmt.Errorf("SPIFFE ID path %q is not a satellite", path)
	}

	// Path format: /satellite/... - split and extract
	parts := strings.Split(path, "/")
	// parts[0] = ""
	// parts[1] = "satellite"

	if len(parts) == 3 {
		// Format: /satellite/<name>
		return parts[2], nil
	}

	if len(parts) >= 5 && parts[2] == "region" {
		// Format: /satellite/region/<region>/<name>
		return parts[4], nil
	}

	// Fall back to last path component
	return parts[len(parts)-1], nil
}

// ExtractRegionFromSPIFFEID extracts the region from a satellite SPIFFE ID.
func ExtractRegionFromSPIFFEID(id spiffeid.ID) (string, error) {
	path := id.Path()
	if !strings.HasPrefix(path, "/satellite/region/") {
		return "", fmt.Errorf("SPIFFE ID path %q does not contain region", path)
	}

	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		return "", fmt.Errorf("invalid satellite SPIFFE ID path: %q", path)
	}

	return parts[3], nil
}

// BuildSatelliteSPIFFEID constructs a SPIFFE ID for a satellite.
func BuildSatelliteSPIFFEID(trustDomain spiffeid.TrustDomain, region, name string) (spiffeid.ID, error) {
	path := fmt.Sprintf("/satellite/region/%s/%s", region, name)
	return spiffeid.FromPath(trustDomain, path)
}

// BuildGroundControlSPIFFEID constructs a SPIFFE ID for Ground Control.
func BuildGroundControlSPIFFEID(trustDomain spiffeid.TrustDomain) (spiffeid.ID, error) {
	return spiffeid.FromPath(trustDomain, "/gc/main")
}
