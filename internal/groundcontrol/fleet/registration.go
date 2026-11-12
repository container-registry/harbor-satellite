package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/crypto"
)

// DefaultConfigName is the config SPIFFE auto-registration assigns; it is
// created when missing.
const DefaultConfigName = "default"

// DefaultConfigDocument is the document of a created default config.
var DefaultConfigDocument = json.RawMessage(`{
  "app_config": {
    "log_level": "info",
    "state_replication_interval": "@every 00h00m30s",
    "register_satellite_interval": "@every 00h00m05s",
    "heartbeat_interval": "@every 00h00m30s"
  }
}`)

// Attestation methods of RegisterSPIFFECmd.
const (
	AttestJoinToken = "join_token"
	AttestX509PoP   = "x509pop"
	AttestSSHPoP    = "sshpop"
)

type RegistrationService struct{ c *core }

type ZTRResult struct {
	SatelliteName, StateURL string
	// RegistryURL is the registry the credentials are for.
	RegistryURL string
	Credentials RobotCredentials
	// Session is the satellite's Ground Control session (ADR-0016); token
	// ZTR issues one, SPIFFE ZTR none because the SVID authenticates.
	Session *SessionGrant
}

type RegisterSPIFFECmd struct {
	Name, ConfigName, Region, AttestationMethod, ParentAgentID string
	Groups, Selectors                                          []string
	// TTL is the join token's lifetime.
	TTL time.Duration
}

type SPIFFERegistration struct {
	SPIFFEID, EntryID, JoinToken string
	// AgentID is the SPIFFE ID of the agent the entry is parented to.
	AgentID   string
	ExpiresAt time.Time
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
func (s *RegistrationService) ZeroTouch(ctx context.Context, token string) (res ZTRResult, err error) {
	repo := s.c.fleetRepo
	id, err := repo.Autocommit().Tokens().Claim(ctx, token, s.c.clock.Now())
	if err != nil {
		return ZTRResult{}, err
	}
	defer func() {
		if err == nil {
			return
		}
		if uerr := repo.Autocommit().Tokens().Unclaim(context.WithoutCancel(ctx), token); uerr != nil {
			err = errors.Join(err, fmt.Errorf("unclaim registration token: %w", uerr))
		}
	}()
	sat, err := repo.Autocommit().Satellites().Get(ctx, id)
	if err != nil {
		return ZTRResult{}, fmt.Errorf("satellite of registration token: %w", err)
	}
	return s.rotate(ctx, sat, func(tx Tx) error {
		if err := tx.Tokens().Consume(ctx, token); err != nil {
			return fmt.Errorf("consume registration token: %w", err)
		}
		return nil
	})
}

// rotate gives the satellite's robot a fresh secret and stores its hash,
// with also run in that transaction. A satellite whose robot the async
// plane has not created yet is ErrNotProvisioned.
func (s *RegistrationService) rotate(ctx context.Context, sat Satellite, also func(Tx) error) (ZTRResult, error) {
	repo := s.c.fleetRepo
	robot, err := repo.Autocommit().Robots().BySatellite(ctx, sat.ID)
	if errors.Is(err, ErrNotFound) {
		return ZTRResult{}, fmt.Errorf("robot of satellite %s: %w", sat.Name, ErrNotProvisioned)
	}
	if err != nil {
		return ZTRResult{}, fmt.Errorf("robot of satellite %s: %w", sat.Name, err)
	}
	creds, err := s.c.registry.RotateRobotSecret(ctx, robot.HarborID)
	if err != nil {
		return ZTRResult{}, fmt.Errorf("rotate robot secret of %s: %w", sat.Name, err)
	}
	hash, err := crypto.HashSecret(creds.Secret)
	if err != nil {
		return ZTRResult{}, fmt.Errorf("hash robot secret: %w", err)
	}
	robot.SecretHash = hash
	if !creds.ExpiresAt.IsZero() {
		robot.ExpiresAt = creds.ExpiresAt
	}
	if creds.Name == "" {
		creds.Name = robot.Name
	}
	err = repo.WithTx(ctx, func(tx Tx) error {
		if err := tx.Robots().Update(ctx, robot); err != nil {
			return fmt.Errorf("store robot secret hash: %w", err)
		}
		if also != nil {
			return also(tx)
		}
		return nil
	})
	if err != nil {
		return ZTRResult{}, err
	}
	stateRef, err := s.c.settings.SatelliteStateRef(sat.Name)
	if err != nil {
		return ZTRResult{}, err
	}
	return ZTRResult{SatelliteName: sat.Name, StateURL: stateRef.String(), RegistryURL: s.c.settings.RegistryURL, Credentials: creds}, nil
}

// SPIFFEZeroTouch registers a satellite that presented an SVID, creating
// it with DefaultConfigName when it is unknown. A revoked satellite is
// ErrRevoked. It stores the SVID's SPIFFE ID with SetIdentity and issues
// no session: the SVID authenticates every beat. A satellite it created
// has no robot until the async plane made one: ErrNotProvisioned.
func (s *RegistrationService) SPIFFEZeroTouch(ctx context.Context, satelliteName string) (ZTRResult, error) {
	repo := s.c.fleetRepo
	sat, err := repo.Autocommit().Satellites().GetByName(ctx, satelliteName)
	if errors.Is(err, ErrNotFound) {
		err = repo.WithTx(ctx, func(tx Tx) error {
			_, err := s.createSPIFFESatellite(ctx, tx, satelliteName, "", nil)
			return err
		})
		if err != nil {
			return ZTRResult{}, err
		}
		return ZTRResult{}, fmt.Errorf("satellite %s: %w", satelliteName, ErrNotProvisioned)
	}
	if err != nil {
		return ZTRResult{}, fmt.Errorf("satellite %s: %w", satelliteName, err)
	}
	res, err := s.rotate(ctx, sat, nil)
	if errors.Is(err, ErrNotProvisioned) {
		// A satellite created before the async plane has no robot and no
		// event that would make one; this asks the reconciler for it.
		if aerr := repo.WithTx(ctx, func(tx Tx) error {
			return tx.Events().Append(ctx, satelliteEvent(EventSatelliteChanged, sat))
		}); aerr != nil {
			return ZTRResult{}, errors.Join(err, aerr)
		}
	}
	return res, err
}

// createSPIFFESatellite creates the satellite with its config, the
// default one created when missing, and its groups.
func (s *RegistrationService) createSPIFFESatellite(ctx context.Context, tx Tx, name, configName string, groups []string) (Satellite, error) {
	sat, err := tx.Satellites().Create(ctx, name)
	if err != nil {
		return Satellite{}, fmt.Errorf("create satellite %s: %w", name, err)
	}
	if configName == "" {
		configName = DefaultConfigName
	}
	cfg, err := tx.Configs().GetByName(ctx, configName)
	if errors.Is(err, ErrNotFound) && configName == DefaultConfigName {
		cfg, err = tx.Configs().Create(ctx, Config{Name: DefaultConfigName, RegistryURL: s.c.settings.RegistryURL, Document: DefaultConfigDocument})
		if err == nil {
			err = tx.Events().Append(ctx, configEvent(EventConfigChanged, cfg))
		}
	}
	if err != nil {
		return Satellite{}, fmt.Errorf("config %s: %w", configName, err)
	}
	if err := tx.Satellites().SetConfig(ctx, sat.ID, cfg.ID); err != nil {
		return Satellite{}, fmt.Errorf("assign config %s: %w", configName, err)
	}
	if err := addToGroups(ctx, tx, sat.ID, groups); err != nil {
		return Satellite{}, err
	}
	if err := tx.Events().Append(ctx, satelliteEvent(EventSatelliteCreated, sat)); err != nil {
		return Satellite{}, err
	}
	return sat, nil
}

// RegisterSPIFFE creates the SPIRE registration of a satellite (a join
// token for join_token, the parent agent for x509pop and sshpop, and the
// workload entry), creates the satellite when it is new and stores the
// SPIFFE ID and the entry ID with SetIdentity. A failure after the entry
// was created deletes it again.
func (s *RegistrationService) RegisterSPIFFE(ctx context.Context, cmd RegisterSPIFFECmd) (SPIFFERegistration, error) {
	if s.c.identity == nil {
		return SPIFFERegistration{}, fmt.Errorf("SPIRE server not configured: %w", ErrNotProvisioned)
	}
	if !ValidName(cmd.Name) {
		return SPIFFERegistration{}, fmt.Errorf("%w: satellite name %q", ErrInvalid, cmd.Name)
	}
	region := cmd.Region
	if region == "" {
		region = "default"
	}
	trustDomain := s.c.identity.TrustDomain()
	var reg SPIFFERegistration
	switch cmd.AttestationMethod {
	case AttestJoinToken:
		reg.AgentID = fmt.Sprintf("spiffe://%s/agent/%s", trustDomain, cmd.Name)
		ttl := cmd.TTL
		if ttl <= 0 {
			ttl = 10 * time.Minute
		}
		token, err := s.c.identity.CreateJoinToken(ctx, reg.AgentID, ttl)
		if err != nil {
			return SPIFFERegistration{}, fmt.Errorf("create join token: %w", err)
		}
		reg.JoinToken = token
		reg.ExpiresAt = s.c.clock.Now().Add(ttl)
	case AttestX509PoP:
		reg.AgentID = cmd.ParentAgentID
		if reg.AgentID == "" {
			agents, err := s.c.identity.ListAgents(ctx, AttestX509PoP)
			if err != nil {
				return SPIFFERegistration{}, fmt.Errorf("list agents: %w", err)
			}
			want := "x509pop:subject:cn:" + cmd.Name
			for _, a := range agents {
				if slices.Contains(a.Selectors, want) {
					reg.AgentID = a.SPIFFEID
					break
				}
			}
			if reg.AgentID == "" {
				return SPIFFERegistration{}, fmt.Errorf("no x509pop agent found with selector %q: %w", want, ErrNotFound)
			}
		}
	case AttestSSHPoP:
		if cmd.ParentAgentID == "" {
			return SPIFFERegistration{}, fmt.Errorf("%w: parent_agent_id is required for sshpop attestation", ErrInvalid)
		}
		reg.AgentID = cmd.ParentAgentID
	default:
		return SPIFFERegistration{}, fmt.Errorf("%w: attestation method %q", ErrInvalid, cmd.AttestationMethod)
	}
	reg.SPIFFEID = fmt.Sprintf("spiffe://%s/satellite/region/%s/%s", trustDomain, region, cmd.Name)
	entryID, err := s.c.identity.CreateEntry(ctx, reg.AgentID, reg.SPIFFEID, cmd.Selectors)
	if err != nil {
		return SPIFFERegistration{}, fmt.Errorf("create workload entry: %w", err)
	}
	reg.EntryID = entryID
	err = s.c.repo.WithTx(ctx, func(tx Tx) error {
		sat, err := tx.Satellites().GetByName(ctx, cmd.Name)
		if errors.Is(err, ErrNotFound) {
			sat, err = s.createSPIFFESatellite(ctx, tx, cmd.Name, cmd.ConfigName, cmd.Groups)
		}
		if err != nil {
			return err
		}
		return tx.Satellites().SetIdentity(ctx, sat.ID, reg.SPIFFEID, []string{entryID})
	})
	if err != nil {
		if derr := s.c.identity.DeleteEntry(context.WithoutCancel(ctx), entryID); derr != nil {
			err = errors.Join(err, fmt.Errorf("delete workload entry: %w", derr))
		}
		return SPIFFERegistration{}, err
	}
	return reg, nil
}

// RefreshCredentials rotates the satellite's robot secret (D2).
func (s *RegistrationService) RefreshCredentials(ctx context.Context, satelliteName string) (RobotCredentials, error) {
	sat, err := s.c.fleetRepo.Autocommit().Satellites().GetByName(ctx, satelliteName)
	if err != nil {
		return RobotCredentials{}, fmt.Errorf("satellite %s: %w", satelliteName, err)
	}
	res, err := s.rotate(ctx, sat, nil)
	if err != nil {
		return RobotCredentials{}, err
	}
	return res.Credentials, nil
}
