package server

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/crypto"
	auditlog "github.com/container-registry/harbor-satellite/internal/shared/logger"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/middleware"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	"github.com/container-registry/harbor-satellite/internal/shared/edgejournal"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
)

// SatelliteAuthenticator is the satellite half of authn.Sessions; app
// passes it with WithSatelliteAuthenticator, so the server imports no gc-core package.
type SatelliteAuthenticator interface {
	Authenticate(ctx context.Context, token string) (fleet.SatelliteAuth, error)
	Lookup(ctx context.Context, satelliteName string) (fleet.SatelliteAuth, error)
}

// WithSatelliteAuthenticator switches the satellite endpoints to the
// session path of plan item 4.1 (ADR-0016). Without it they keep today's
// chain.
func WithSatelliteAuthenticator(a SatelliteAuthenticator) Option {
	return func(s *Server) { s.satelliteAuth = a }
}

type satelliteAuthKey struct{}

// authenticateSatellite is the one authentication step of the session path:
// an SVID name in the context goes to Lookup (Method svid); any Bearer token
// to Authenticate; Basic, while SATELLITE_ROBOT_AUTH is on, to
// validateRobotAuth and then Lookup (Method robot). The FailureLimiter is
// checked by client IP before the Argon2id verify, and every failure is
// recorded in it. The auth is stored under satelliteAuthKey, then the fleet
// limiter and the handler run; nothing authenticates again.
func (s *Server) authenticateSatellite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ip := s.clientAddr(r)
		var (
			auth      fleet.SatelliteAuth
			err       error
			actor     string
			actorType = auditlog.ActorSatellite
		)
		switch {
		case hasSVIDName(ctx):
			name, _ := spiffe.GetSatelliteName(ctx)
			actor = name
			auth, err = s.satelliteAuth.Lookup(ctx, name)
			auth.Method = fleet.AuthSVID
		case bearerToken(r) != "":
			auth, err = s.satelliteAuth.Authenticate(ctx, bearerToken(r))
			actor = auth.SatelliteName
		default:
			username, password, ok := extractBasicAuth(r)
			if !ok || !env.GC.Server.SatelliteRobotAuth {
				s.FailureLimiter.RecordFailure(ip)
				s.failAuth(w, r, username, auditlog.ActorAnonymous, auditlog.ReasonMissingCredentials, "Unauthorized")
				return
			}
			if s.FailureLimiter.Blocked(ip) {
				middleware.WriteTooManyRequests(w)
				return
			}
			actor, actorType = username, auditlog.ActorRobot
			var name string
			if name, err = s.robotSatellite(ctx, username, password); err == nil {
				auth, err = s.satelliteAuth.Lookup(ctx, name)
				auth.Method = fleet.AuthRobot
			}
		}
		if errors.Is(err, fleet.ErrRevoked) {
			s.auditEvent(r, auditlog.AuditEvent{
				Operation: auditlog.OpAuth, ResourceType: auditlog.ResSatellite, Outcome: auditlog.OutcomeFailure,
				Actor: actor, ActorType: actorType, Reason: auditlog.ReasonForbidden,
			})
			HandleAppError(w, &AppError{Code: http.StatusForbidden, Message: "Error: satellite revoked"})
			return
		}
		if err != nil {
			if !isAuthFailure(err) {
				log.Printf("satellite authentication: %v", err)
				HandleAppError(w, &AppError{Code: http.StatusInternalServerError, Message: "Error: authentication failed"})
				return
			}
			s.FailureLimiter.RecordFailure(ip)
			reason := auditlog.ReasonInvalidCredentials
			if errors.Is(err, errRobotExpired) || errors.Is(err, fleet.ErrSessionExpired) {
				reason = auditlog.ReasonTokenExpired
			}
			s.failAuth(w, r, actor, actorType, reason, "Unauthorized")
			return
		}
		ctx = context.WithValue(ctx, satelliteAuthKey{}, auth)
		ctx = spiffe.ContextWithSatelliteName(ctx, auth.SatelliteName)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// robotSatellite verifies robot Basic credentials in one statement and
// returns the robot's satellite. An unknown robot costs the same Argon2id
// verify as a known one, so timing does not reveal which robots exist.
func (s *Server) robotSatellite(ctx context.Context, username, password string) (string, error) {
	robot, err := s.fleet().GetRobotAuthByRobotName(ctx, username)
	if err != nil {
		_ = crypto.VerifySecret(password, crypto.TimingMitigationHash)
		return "", err
	}
	if !crypto.VerifySecret(password, robot.RobotSecretHash) {
		return "", errRobotInvalidSecret
	}
	if robot.RobotExpiry.Valid && robot.RobotExpiry.Time.Before(time.Now()) {
		return "", errRobotExpired
	}
	return robot.SatelliteName, nil
}

func hasSVIDName(ctx context.Context) bool {
	_, ok := spiffe.GetSatelliteName(ctx)
	return ok
}

func bearerToken(r *http.Request) string {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(token)
}

// isAuthFailure tells credentials that do not authenticate from a failing
// store; only the first count against the client.
func isAuthFailure(err error) bool {
	return errors.Is(err, fleet.ErrSessionInvalid) || errors.Is(err, fleet.ErrSessionExpired) ||
		errors.Is(err, fleet.ErrNotFound) || errors.Is(err, sql.ErrNoRows) ||
		errors.Is(err, errRobotInvalidSecret) || errors.Is(err, errRobotExpired)
}

// syncWithAuth is SyncSatellite on the session path: one
// HeartbeatService.Beat with the SatelliteAuth of the context.
func (s *Server) syncWithAuth(w http.ResponseWriter, r *http.Request) {
	auth, ok := r.Context().Value(satelliteAuthKey{}).(fleet.SatelliteAuth)
	if !ok {
		HandleAppError(w, &AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		return
	}
	var req SatelliteStatusRequest
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}
	res, err := s.svc().Heartbeats.Beat(r.Context(), heartbeatCmd(auth, req))
	if err != nil {
		if StatusForError(err) == http.StatusInternalServerError {
			log.Printf("heartbeat of %s: %v", auth.SatelliteName, err)
		}
		handleFleetError(w, err, nil)
		return
	}
	if res.JournalErr != nil {
		log.Printf("heartbeat of %s answered without a journal ack: %v", auth.SatelliteName, res.JournalErr)
	}
	WriteJSONResponse(w, http.StatusOK, syncResponse(res))
}

func heartbeatCmd(auth fleet.SatelliteAuth, req SatelliteStatusRequest) fleet.HeartbeatCmd {
	cmd := fleet.HeartbeatCmd{
		Auth:     auth,
		Interval: req.StateReportInterval,
		Sample: fleet.StatusSample{
			Satellite: auth.Satellite, Activity: req.Activity,
			LatestStateDigest: req.LatestStateDigest, LatestConfigDigest: req.LatestConfigDigest,
			CPUPercent: req.CPUPercent, MemoryUsedBytes: req.MemoryUsedBytes, StorageUsedBytes: req.StorageUsedBytes,
			LastSyncDuration: req.LastSyncDurationMs, ImageCount: req.ImageCount, ReportedAt: req.RequestCreatedTime,
		},
		StateDigest:           fleet.Digest(req.LatestStateDigest),
		ConfigDigest:          fleet.Digest(req.LatestConfigDigest),
		LastReplicationError:  req.LastReplicationError,
		InventoryDigest:       fleet.Digest(req.InventoryDigest),
		CredentialsGeneration: req.CredentialsGeneration,
	}
	for _, a := range req.AppliedStates {
		cmd.AppliedStates = append(cmd.AppliedStates, fleet.AppliedState{Ref: a.Ref, Digest: fleet.Digest(a.Digest)})
	}
	if req.CachedImages != nil {
		cmd.Inventory = make([]fleet.InventoryItem, 0, len(req.CachedImages))
		for _, img := range req.CachedImages {
			cmd.Inventory = append(cmd.Inventory, fleet.InventoryItem{Reference: img.Reference, SizeBytes: img.SizeBytes})
		}
	}
	if j := req.Journal; j.Epoch != 0 || j.AfterSeq != 0 || len(j.Records) > 0 {
		upload := &fleet.JournalUpload{Epoch: j.Epoch, AfterSeq: j.AfterSeq, Records: make([]fleet.JournalRecord, 0, len(j.Records))}
		for _, rec := range j.Records {
			upload.Records = append(upload.Records, fleet.JournalRecord{Seq: rec.Seq, Type: edgejournal.RecordType(rec.Type), RecordedAt: rec.RecordedAt, Payload: rec.Payload})
		}
		cmd.Journal = upload
	}
	return cmd
}

func syncResponse(res fleet.HeartbeatResult) SatelliteSyncResponse {
	out := SatelliteSyncResponse{Events: res.Events, InventoryRequested: res.InventoryRequested}
	if out.Events == nil {
		out.Events = []string{}
	}
	if res.Journal != nil {
		out.Journal = &JournalAck{Epoch: res.Journal.Epoch, AckedSeq: res.Journal.AckedSeq, Error: res.Journal.Error}
	}
	if res.Session != nil {
		out.Session = &SatelliteSession{Token: res.Session.Token, ExpiresAt: res.Session.ExpiresAt}
	}
	if c := res.Credentials; c != nil {
		out.Credentials = &RotatedCredentials{Username: c.Username, Password: c.Password, ExpiresAt: c.ExpiresAt, Generation: c.Generation}
	}
	return out
}

// fleetLimit limits the satellite endpoints per authenticated satellite,
// so satellites behind one NAT do not share a budget (A66, A124).
func (s *Server) fleetLimit(next http.Handler) http.Handler {
	if s.FleetLimiter == nil {
		return next
	}
	return middleware.LimitBy(s.FleetLimiter, func(r *http.Request) (fleet.SatelliteID, bool) {
		auth, ok := r.Context().Value(satelliteAuthKey{}).(fleet.SatelliteAuth)
		return auth.Satellite, ok
	})(next)
}

// limitAnonymous limits an unauthenticated endpoint per client IP. Before
// RATE_LIMIT_SPLIT it is the shared legacy limiter; after it the key is
// clientAddr.
func (s *Server) limitAnonymous(next http.Handler) http.Handler {
	if s.AnonymousLimiter == nil {
		return middleware.RateLimitMiddleware(s.rateLimiter)(next)
	}
	return middleware.LimitBy(s.AnonymousLimiter, func(r *http.Request) (netip.Addr, bool) {
		return s.clientAddr(r), true
	})(next)
}
