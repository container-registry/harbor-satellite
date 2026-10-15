package fleet

import (
	"context"
	"time"
)

// RobotAccounts manages the satellites' robot accounts in the registry.
type RobotAccounts interface {
	// FindRobot returns the robot with this name, without its secret, or
	// ErrNotFound.
	FindRobot(ctx context.Context, name string) (RobotCredentials, error)
	// CreateRobot fails with ErrAlreadyExists when a robot of that name
	// exists. The lifetime is spec.Duration.
	CreateRobot(ctx context.Context, spec RobotSpec) (RobotCredentials, error)
	// SetRobotProjects replaces the robot's pull permissions with exactly
	// projects.
	SetRobotProjects(ctx context.Context, harborID int64, projects []string) error
	RotateRobotSecret(ctx context.Context, harborID int64) (RobotCredentials, error)
	DeleteRobot(ctx context.Context, harborID int64) error
}

// StateArtifacts publishes the state documents satellites fetch.
type StateArtifacts interface {
	// PushState stores payload as artifacts.json at ref, tags it with the
	// push time too and returns the manifest digest.
	PushState(ctx context.Context, ref ArtifactRef, payload []byte) (Digest, error)
	DeleteState(ctx context.Context, ref ArtifactRef) error
}

type Projects interface {
	EnsureProject(ctx context.Context, project string) error
	ReplicationPolicyExists(ctx context.Context, name string) (bool, error)
}

// Registry is the upstream registry. Adapters return ErrRegistryUnavailable
// when it cannot be reached, and ErrNotFound when the robot, project or
// state artifact a call names does not exist, so the async plane can treat
// an already deleted object as deleted. It has interfacebloat's 10
// methods; robot rotation (plan item 8.4) gets its own port.
type Registry interface {
	RobotAccounts
	StateArtifacts
	Projects
	Ping(ctx context.Context) error
}

// WorkloadIdentity is the SPIFFE workload identity provider (SPIRE).
type WorkloadIdentity interface {
	TrustDomain() string
	CreateJoinToken(ctx context.Context, spiffeID string, ttl time.Duration) (string, error)
	CreateEntry(ctx context.Context, parentID, spiffeID string, selectors []string) (string, error)
	DeleteEntry(ctx context.Context, entryID string) error
	// DeleteEntriesFor deletes every registration entry of spiffeID; none
	// is not an error.
	DeleteEntriesFor(ctx context.Context, spiffeID string) error
	ListAgents(ctx context.Context, attestationType string) ([]Agent, error)
}

// Repository is the fleet's unit of work. Lookups return ErrNotFound,
// creates ErrAlreadyExists.
type Repository interface {
	// Autocommit returns a Tx that commits each call on its own. Its
	// Events() rejects appends with ErrOutsideTransaction: an event must
	// commit with the change it describes.
	Autocommit() Tx
	// WithTx runs fn in one transaction, committing when fn returns nil.
	WithTx(ctx context.Context, fn func(tx Tx) error) error
}

// Tx is the repository inside a transaction. It stays within
// interfacebloat's 10 methods; journal, views and policies add one each.
type Tx interface {
	Satellites() SatelliteRepo
	Groups() GroupRepo
	Configs() ConfigRepo
	Robots() RobotRepo
	Tokens() TokenRepo
	// Events appends to the transactional outbox; a NopEventSink until
	// plan item 2.
	Events() EventSink
}

type SatelliteRepo interface {
	Create(ctx context.Context, name string) (Satellite, error)
	Get(ctx context.Context, id SatelliteID) (Satellite, error)
	GetByName(ctx context.Context, name string) (Satellite, error)
	List(ctx context.Context) ([]Satellite, error)
	Delete(ctx context.Context, id SatelliteID) error
	Groups(ctx context.Context, id SatelliteID) ([]Group, error)
	// RobotGrants returns the robot and the group projects of every given
	// satellite that has a robot, in one statement however many satellites
	// there are, so a group change costs the same for 1 and 100 members.
	RobotGrants(ctx context.Context, ids []SatelliteID) ([]RobotGrant, error)
	SetConfig(ctx context.Context, id SatelliteID, config ConfigID) error
	Config(ctx context.Context, id SatelliteID) (Config, error)
}

type GroupRepo interface {
	// Upsert creates the group or replaces its projects and artifacts.
	Upsert(ctx context.Context, group Group) (Group, error)
	Get(ctx context.Context, id GroupID) (Group, error)
	GetByName(ctx context.Context, name string) (Group, error)
	List(ctx context.Context) ([]Group, error)
	Delete(ctx context.Context, id GroupID) error
	Members(ctx context.Context, id GroupID) ([]SatelliteID, error)
	AddMember(ctx context.Context, id GroupID, satellite SatelliteID) error
	RemoveMember(ctx context.Context, id GroupID, satellite SatelliteID) error
}

type ConfigRepo interface {
	Create(ctx context.Context, config Config) (Config, error)
	Get(ctx context.Context, id ConfigID) (Config, error)
	GetByName(ctx context.Context, name string) (Config, error)
	List(ctx context.Context) ([]Config, error)
	Update(ctx context.Context, config Config) (Config, error)
	Delete(ctx context.Context, id ConfigID) error
	InUse(ctx context.Context, id ConfigID) (bool, error)
}

type RobotRepo interface {
	Add(ctx context.Context, robot Robot) (Robot, error)
	BySatellite(ctx context.Context, satellite SatelliteID) (Robot, error)
	ByName(ctx context.Context, name string) (Robot, error)
	Update(ctx context.Context, robot Robot) error
	Delete(ctx context.Context, id RobotID) error
}

// TokenRepo holds single-use registration tokens (D7): a registration
// claims its token, then consumes it or, when a later step fails,
// unclaims it so the satellite can retry.
type TokenRepo interface {
	Add(ctx context.Context, satellite SatelliteID, token string, expiresAt time.Time) error
	// Claim takes the token in one statement, so exactly one of several
	// concurrent registrations wins. A missing or already claimed token is
	// ErrTokenInvalid, an expired one ErrTokenExpired. now comes from the
	// caller's clock because GC writes expires_at as a TIMESTAMP.
	Claim(ctx context.Context, token string, now time.Time) (SatelliteID, error)
	Unclaim(ctx context.Context, token string) error
	Consume(ctx context.Context, token string) error
}
