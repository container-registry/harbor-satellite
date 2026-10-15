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
func (s *RegistrationService) ZeroTouch(ctx context.Context, token string) (ZTRResult, error) {
	return ZTRResult{}, notimpl.Err("1.2 zero-touch registration")
}

// SPIFFEZeroTouch registers a satellite that presented an SVID, creating
// it with DefaultConfigName when it is unknown.
func (s *RegistrationService) SPIFFEZeroTouch(ctx context.Context, satelliteName string) (ZTRResult, error) {
	return ZTRResult{}, notimpl.Err("1.2 SPIFFE zero-touch registration")
}

func (s *RegistrationService) RegisterSPIFFE(ctx context.Context, cmd RegisterSPIFFECmd) (SPIFFERegistration, error) {
	return SPIFFERegistration{}, notimpl.Err("1.2 SPIFFE registration")
}

// RefreshCredentials rotates the satellite's robot secret (D2).
func (s *RegistrationService) RefreshCredentials(ctx context.Context, satelliteName string) (RobotCredentials, error) {
	return RobotCredentials{}, notimpl.Err("1.2 refresh credentials")
}
