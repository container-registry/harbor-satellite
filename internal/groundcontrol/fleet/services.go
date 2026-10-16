package fleet

import "github.com/container-registry/harbor-satellite/internal/shared/notimpl"

// Deps are the ports and settings the services run on. Identity is nil
// when SPIRE is not configured.
type Deps struct {
	Repository Repository
	Registry   Registry
	Identity   WorkloadIdentity
	// Policy authorizes the principal of the request context (A18's
	// scoped checks); nil is AllowAll.
	Policy Policy
	// ConfigValidator checks config documents before they are stored; nil
	// stores them unchecked, as today.
	ConfigValidator ConfigValidator
	Clock           Clock
	Settings        Settings
	// Sessions issues and invalidates satellite sessions (ADR-0016).
	Sessions SessionIssuer
	// HeartbeatSink is this replica's heartbeat buffer.
	HeartbeatSink HeartbeatSink
	// SatelliteRepository is the fleet pool's repository for registration,
	// heartbeats and the journal; nil means Repository.
	SatelliteRepository Repository
	// Rotations answers pending robot rotations from the replica's cache
	// and Sealer seals successor secrets (plan item 8.4); nil Rotations
	// delivers none.
	Rotations RotationSource
	Sealer    Sealer
}

// Services is what the HTTP handlers call: each handler decodes, calls one
// service method and encodes the API type (plan item 1.4).
type Services struct {
	Satellites   *SatelliteService
	Groups       *GroupService
	Configs      *ConfigService
	Registration *RegistrationService
	// Heartbeats owns the JournalService (plan items 4.2 and 6.5).
	Heartbeats *HeartbeatService
	Policies   *PolicyService
}

// NewServices fails with ErrInvalid when Repository or Registry is
// missing; a missing Clock is SystemClock and a missing Policy AllowAll.
// It builds the JournalService on SatelliteRepository and hands it to the
// HeartbeatService only.
//
// Every admin method authorizes PrincipalFrom(ctx) with Policy and fails
// with ErrForbidden when the policy denies (plan item 7.3, A118). A method
// on one satellite first loads the satellite's groups with
// Groups().GroupsOfSatellites, one statement, and passes them as the
// resource's Parents; Register takes its parents from the command and so
// authorizes before any read. Register and ConfigService.Assign also need
// add_member on every group they touch, so a scoped admin cannot reach a
// group outside its scope through them. RegisterSPIFFE authorizes
// register_spiffe on ResourceSPIRE, which no scoped rule may name, because
// it creates SPIRE entries and join tokens. List methods load the parents of
// all items in that one statement and return only the items the policy
// allows to read. Every check precedes the first write, and a policy
// failure is returned as is, never as ErrForbidden. The satellite-facing
// methods are not checked.
func NewServices(deps Deps) (*Services, error) {
	return nil, notimpl.Err("1.2 fleet services")
}
