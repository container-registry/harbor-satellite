package fleet

import "github.com/container-registry/harbor-satellite/internal/shared/notimpl"

// Deps are the ports and settings the services run on. Identity is nil
// when SPIRE is not configured.
type Deps struct {
	Repository Repository
	Registry   Registry
	Identity   WorkloadIdentity
	// Policy authorizes the principal of the request context (A18's
	// scoped checks); nil is AllowAll until plan item 7.
	Policy   Policy
	Clock    Clock
	Settings Settings
}

// Services is what the HTTP handlers call: each handler decodes, calls one
// service method and encodes the API type (plan item 1.4).
type Services struct {
	Satellites   *SatelliteService
	Groups       *GroupService
	Configs      *ConfigService
	Registration *RegistrationService
}

// NewServices fails with ErrInvalid when Repository or Registry is
// missing; a missing Clock is SystemClock and a missing Policy AllowAll.
// Every method authorizes PrincipalFrom(ctx) with Policy before it reads
// or writes, and fails with ErrForbidden when the policy denies.
func NewServices(deps Deps) (*Services, error) {
	return nil, notimpl.Err("1.2 fleet services")
}
