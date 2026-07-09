package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	_ "github.com/joho/godotenv/autoload"
	_ "github.com/lib/pq"

	gcauth "github.com/container-registry/harbor-satellite/internal/groundcontrol/auth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
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
	passwordPolicy  gcauth.PasswordPolicy
	sessionDuration time.Duration
	lockoutDuration time.Duration
}

var (
	serviceOnce sync.Once
	serviceInst *service
	serviceErr  error
)

func getService() (*service, error) {
	serviceOnce.Do(func() {
		connStr := fmt.Sprintf(
			"postgres://%s:%s@%s/%s?sslmode=disable",
			os.Getenv("DB_USERNAME"),
			os.Getenv("DB_PASSWORD"),
			net.JoinHostPort(os.Getenv("DB_HOST"), os.Getenv("DB_PORT")),
			os.Getenv("DB_DATABASE"),
		)

		db, err := sql.Open("postgres", connStr)
		if err != nil {
			serviceErr = err
			return
		}

		serviceInst = &service{
			db:              db,
			queries:         database.New(db),
			passwordPolicy:  gcauth.LoadPolicyFromEnv(),
			sessionDuration: parseDurationEnv("SESSION_DURATION", 24*time.Hour),
			lockoutDuration: parseDurationEnv("LOCKOUT_DURATION", 5*time.Minute),
		}
	})

	return serviceInst, serviceErr
}

func parseDurationEnv(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return value
}

func appError(message string, code int) *swaggermodels.AppError {
	return &swaggermodels.AppError{Message: message, Code: int64(code)}
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

func requireRole(principal any, role string) (principalUser, *swaggermodels.AppError) {
	user, errPayload := requirePrincipal(principal)
	if errPayload != nil {
		return principalUser{}, errPayload
	}
	if user.Role != role {
		return principalUser{}, appError("Forbidden", http.StatusForbidden)
	}
	return user, nil
}

func AuthenticateBearer(token string) (any, error) {
	svc, err := getService()
	if err != nil {
		return nil, err
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
