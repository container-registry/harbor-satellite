package fleet

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// DefaultConfigName is the config SPIFFE auto-registration assigns; it is
// created when missing.
const DefaultConfigName = "default"

type RegistrationService struct{}

type ZTRResult struct {
	SatelliteName, StateURL string
	Credentials             RobotCredentials
	// Session is the satellite's Ground Control session (ADR-0016); token
	// ZTR issues one, SPIFFE ZTR none because the SVID authenticates.
	Session *SessionGrant
}

type RegisterSPIFFECmd struct {
	Name, ConfigName, Region, AttestationMethod, ParentAgentID string
	Groups, Selectors                                          []string
}

type SPIFFERegistration struct {
	SPIFFEID, EntryID, JoinToken string
	ExpiresAt                    time.Time
}

// ZeroTouch claims the token, rotates the robot secret, republishes the
// satellite's state and consumes the token. When a step after the claim
// fails the token is unclaimed, so the satellite can retry with it. The
// claim commits on its own (Repository.Autocommit) and the secret is
// rotated before any transaction opens, so a concurrent caller is never
// held on a row lock across a registry round trip.
//
// Plan item 4.1 runs, in order: the claim; Views().Get, where a revoked
// satellite unclaims and is ErrRevoked before any rotation; the robot
// lookup (ErrNotProvisioned unclaims); RotateRobotSecret;
// Sessions.Issue; WithTx. A failure after the claim unclaims the token
// and invalidates the issued session.
func (s *RegistrationService) ZeroTouch(ctx context.Context, token string) (ZTRResult, error) {
	return ZTRResult{}, notimpl.Err("1.2 zero-touch registration")
}

// SPIFFEZeroTouch registers a satellite that presented an SVID, creating
// it with DefaultConfigName when it is unknown. A revoked satellite is
// ErrRevoked. It stores the SVID's SPIFFE ID with SetIdentity and issues
// no session: the SVID authenticates every beat.
func (s *RegistrationService) SPIFFEZeroTouch(ctx context.Context, satelliteName string) (ZTRResult, error) {
	return ZTRResult{}, notimpl.Err("1.2 SPIFFE zero-touch registration")
}

// RegisterSPIFFE stores the SPIFFE ID and the entry ID with SetIdentity.
func (s *RegistrationService) RegisterSPIFFE(ctx context.Context, cmd RegisterSPIFFECmd) (SPIFFERegistration, error) {
	return SPIFFERegistration{}, notimpl.Err("1.2 SPIFFE registration")
}

// RefreshCredentials rotates the satellite's robot secret (D2).
func (s *RegistrationService) RefreshCredentials(ctx context.Context, satelliteName string) (RobotCredentials, error) {
	return RobotCredentials{}, notimpl.Err("1.2 refresh credentials")
}
