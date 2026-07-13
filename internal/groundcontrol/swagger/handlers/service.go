package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	_ "github.com/joho/godotenv/autoload"
	_ "github.com/lib/pq"

	"github.com/container-registry/harbor-satellite/internal/env"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/auth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	auditlog "github.com/container-registry/harbor-satellite/internal/groundcontrol/logger"
	gcmiddleware "github.com/container-registry/harbor-satellite/internal/groundcontrol/middleware"
	swaggermodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/models"
)

const (
	roleAdmin         = "admin"
	roleSystemAdmin   = "system_admin"
	maxFailedAttempts = 5
)

type principalUser struct {
	ID       int32
	Username string
	Role     string
}

type service struct {
	db              *sql.DB
	queries         *database.Queries
	passwordPolicy  auth.PasswordPolicy
	sessionDuration time.Duration
	lockoutDuration time.Duration
	audit           *auditlog.AuditLogger
	rateLimiter     *gcmiddleware.RateLimiter

	trustForwardedHeaders bool

	lifecycleMu   sync.Mutex
	cleanupCancel context.CancelFunc
	cleanupWG     sync.WaitGroup
	shutdownOnce  sync.Once
	shutdownErr   error
}

var (
	serviceOnce sync.Once
	serviceInst *service
	errService  error
)

func getService() (*service, error) {
	serviceOnce.Do(func() {
		auditConfig, err := env.GC.Audit.Config()
		if err != nil {
			errService = err
			return
		}
		auditLogger, err := auditlog.NewAuditLogger(auditConfig, auditlog.ComponentGroundControl)
		if err != nil {
			errService = err
			return
		}

		connStr := env.GC.Database.URL()

		db, err := sql.Open("postgres", connStr)
		if err != nil {
			errService = err
			return
		}

		serviceInst = &service{
			db:              db,
			queries:         database.New(db),
			passwordPolicy:  auth.LoadPolicyFromEnv(),
			sessionDuration: env.GC.Server.SessionDuration,
			lockoutDuration: env.GC.Server.LockoutDuration,
			audit:           auditLogger,
			rateLimiter:     gcmiddleware.NewRateLimiter(10, time.Minute),

			trustForwardedHeaders: env.GC.Audit.TrustForwardedHeaders,
		}
	})

	return serviceInst, errService
}

func appError(message string, code int) *swaggermodels.AppError {
	return &swaggermodels.AppError{Message: message, Code: int64(code)}
}

func internalError(message string, err error) *swaggermodels.AppError {
	return loggedError(message, http.StatusInternalServerError, err)
}

func upstreamError(message string, err error) *swaggermodels.AppError {
	return loggedError(message, http.StatusBadGateway, err)
}

func loggedError(message string, code int, err error) *swaggermodels.AppError {
	if err != nil {
		log.Printf("%s: %v", message, err)
	}
	return appError(message, code)
}

func principalFromAny(principal any) (principalUser, bool) {
	user, ok := principal.(principalUser)
	return user, ok
}

func requirePrincipal(principal any) (principalUser, *swaggermodels.AppError) {
	user, ok := principalFromAny(principal)
	if !ok {
		return principalUser{}, appError("Unauthorized", http.StatusUnauthorized)
	}
	return user, nil
}

func requireSystemAdmin(principal any) (principalUser, *swaggermodels.AppError) {
	user, errPayload := requirePrincipal(principal)
	if errPayload != nil {
		return principalUser{}, errPayload
	}
	if user.Role != roleSystemAdmin {
		return principalUser{}, appError("Forbidden", http.StatusForbidden)
	}
	return user, nil
}

func AuthenticateBearer(token string) (any, error) {
	svc, err := getService()
	if err != nil {
		return nil, err
	}
	token = sessionToken(token)
	if token == "" {
		return nil, errors.New("invalid authorization header")
	}

	session, err := svc.queries.GetSessionByToken(context.Background(), token)
	if err != nil {
		return nil, err
	}

	return principalUser{
		ID:       session.UserID,
		Username: session.Username,
		Role:     session.Role,
	}, nil
}

func (s *service) auditEvent(r *http.Request, event auditlog.AuditEvent) {
	if r != nil {
		event.SourceIP = clientIP(r, s.trustForwardedHeaders)
		event.UserAgent = r.UserAgent()
		if requestID := strings.TrimSpace(r.Header.Get("X-Request-ID")); validAuditRequestID(requestID) {
			event.RequestID = requestID
		}
	}
	s.audit.Log(event)
}

func clientIP(r *http.Request, trustForwardedHeaders bool) string {
	if trustForwardedHeaders {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			if comma := strings.IndexByte(forwarded, ','); comma >= 0 {
				forwarded = forwarded[:comma]
			}
			return strings.TrimSpace(forwarded)
		}
		if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
			return realIP
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func validAuditRequestID(requestID string) bool {
	if len(requestID) == 0 || len(requestID) > 128 {
		return false
	}
	for _, char := range requestID {
		if !validAuditRequestIDCharacter(char) {
			return false
		}
	}
	return true
}

func validAuditRequestIDCharacter(char rune) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
		char >= '0' && char <= '9' || strings.ContainsRune("._-", char)
}
