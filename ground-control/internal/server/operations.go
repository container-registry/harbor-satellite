package server

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/container-registry/harbor-satellite/ground-control/internal/auth"
	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
)

type operationError struct {
	statusCode int
	message    string
}

func (e *operationError) Error() string {
	return e.message
}

type loginResult struct {
	Token     string
	ExpiresAt time.Time
}

type userView struct {
	ID        int32
	Username  string
	Role      string
	CreatedAt time.Time
}

func (s *Server) login(ctx context.Context, username, password string) (loginResult, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return loginResult{}, newOperationError(http.StatusUnauthorized, "Invalid credentials")
	}

	attempts, err := s.dbQueries.GetLoginAttempts(ctx, username)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return loginResult{}, newOperationError(http.StatusInternalServerError, "Internal server error")
	}

	if err == nil && attempts.LockedUntil.Valid && attempts.LockedUntil.Time.After(time.Now()) {
		return loginResult{}, newOperationError(http.StatusUnauthorized, "Invalid credentials")
	}

	user, err := s.dbQueries.GetUserByUsername(ctx, username)
	if err != nil {
		s.recordFailedAttempt(ctx, username)
		return loginResult{}, newOperationError(http.StatusUnauthorized, "Invalid credentials")
	}

	if !auth.VerifyPassword(password, user.PasswordHash) {
		s.recordFailedAttempt(ctx, username)
		return loginResult{}, newOperationError(http.StatusUnauthorized, "Invalid credentials")
	}

	_ = s.dbQueries.ResetLoginAttempts(ctx, username)

	token, err := auth.GenerateSessionToken()
	if err != nil {
		return loginResult{}, newOperationError(http.StatusInternalServerError, "Internal server error")
	}

	expiresAt := time.Now().Add(s.sessionDuration)
	_, err = s.dbQueries.CreateSession(ctx, database.CreateSessionParams{
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return loginResult{}, newOperationError(http.StatusInternalServerError, "Internal server error")
	}

	return loginResult{Token: token, ExpiresAt: expiresAt}, nil
}

func (s *Server) logout(ctx context.Context, token string) error {
	if token == "" {
		return newOperationError(http.StatusUnauthorized, "Unauthorized")
	}

	if err := s.dbQueries.DeleteSession(ctx, token); err != nil {
		return newOperationError(http.StatusInternalServerError, "Internal server error")
	}

	return nil
}

func (s *Server) listUsers(ctx context.Context) ([]userView, error) {
	users, err := s.dbQueries.ListUsers(ctx)
	if err != nil {
		return nil, newOperationError(http.StatusInternalServerError, "Internal server error")
	}

	result := make([]userView, 0, len(users))
	for _, user := range users {
		result = append(result, userView{
			ID:        user.ID,
			Username:  user.Username,
			Role:      user.Role,
			CreatedAt: user.CreatedAt,
		})
	}

	return result, nil
}

func (s *Server) getUser(ctx context.Context, username string) (userView, error) {
	user, err := s.dbQueries.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return userView{}, newOperationError(http.StatusNotFound, "User not found")
		}
		return userView{}, newOperationError(http.StatusInternalServerError, "Internal server error")
	}

	if user.Role == roleSystemAdmin {
		return userView{}, newOperationError(http.StatusNotFound, "User not found")
	}

	return userView{
		ID:        user.ID,
		Username:  user.Username,
		Role:      user.Role,
		CreatedAt: user.CreatedAt,
	}, nil
}

func (s *Server) createUser(ctx context.Context, username, password string) (userView, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return userView{}, newOperationError(http.StatusBadRequest, "Username is required")
	}
	if username == "admin" {
		return userView{}, newOperationError(http.StatusBadRequest, "Username 'admin' is reserved")
	}
	if err := s.passwordPolicy.Validate(password); err != nil {
		return userView{}, newOperationError(http.StatusBadRequest, err.Error())
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return userView{}, newOperationError(http.StatusInternalServerError, "Internal server error")
	}

	user, err := s.dbQueries.CreateUser(ctx, database.CreateUserParams{
		Username:     username,
		PasswordHash: hash,
		Role:         roleAdmin,
	})
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return userView{}, newOperationError(http.StatusConflict, "User already exists")
		}
		return userView{}, newOperationError(http.StatusInternalServerError, "Internal server error")
	}

	return userView{
		ID:        user.ID,
		Username:  user.Username,
		Role:      user.Role,
		CreatedAt: user.CreatedAt,
	}, nil
}

func (s *Server) deleteUser(ctx context.Context, currentUser AuthUser, username string) error {
	if username == currentUser.Username {
		return newOperationError(http.StatusBadRequest, "Cannot delete yourself")
	}
	if username == "admin" {
		return newOperationError(http.StatusBadRequest, "Cannot delete system admin")
	}

	user, err := s.dbQueries.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return newOperationError(http.StatusNotFound, "User not found")
		}
		return newOperationError(http.StatusInternalServerError, "Internal server error")
	}

	if err := s.dbQueries.DeleteUserSessions(ctx, user.ID); err != nil {
		return newOperationError(http.StatusInternalServerError, "Internal server error")
	}
	if err := s.dbQueries.DeleteUser(ctx, username); err != nil {
		return newOperationError(http.StatusInternalServerError, "Internal server error")
	}

	return nil
}

func (s *Server) changeOwnPassword(ctx context.Context, currentUser AuthUser, currentPassword, newPassword string) error {
	if err := s.passwordPolicy.Validate(newPassword); err != nil {
		return newOperationError(http.StatusBadRequest, err.Error())
	}

	user, err := s.dbQueries.GetUserByUsername(ctx, currentUser.Username)
	if err != nil {
		return newOperationError(http.StatusInternalServerError, "Internal server error")
	}

	if !auth.VerifyPassword(currentPassword, user.PasswordHash) {
		return newOperationError(http.StatusUnauthorized, "Current password is incorrect")
	}

	return s.updatePassword(ctx, currentUser.Username, user.ID, newPassword)
}

func (s *Server) changeUserPassword(ctx context.Context, username, newPassword string) error {
	if err := s.passwordPolicy.Validate(newPassword); err != nil {
		return newOperationError(http.StatusBadRequest, err.Error())
	}

	user, err := s.dbQueries.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return newOperationError(http.StatusNotFound, "User not found")
		}
		return newOperationError(http.StatusInternalServerError, "Internal server error")
	}

	return s.updatePassword(ctx, username, user.ID, newPassword)
}

func (s *Server) updatePassword(ctx context.Context, username string, userID int32, password string) error {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return newOperationError(http.StatusInternalServerError, "Internal server error")
	}

	if err := s.dbQueries.UpdateUserPassword(ctx, database.UpdateUserPasswordParams{
		Username:     username,
		PasswordHash: hash,
	}); err != nil {
		return newOperationError(http.StatusInternalServerError, "Internal server error")
	}

	if err := s.dbQueries.DeleteUserSessions(ctx, userID); err != nil {
		return newOperationError(http.StatusInternalServerError, "Internal server error")
	}

	return nil
}

func (s *Server) authenticateBearer(ctx context.Context, token string) (apiPrincipal, error) {
	token = normalizeBearerToken(token)
	if token == "" {
		return apiPrincipal{}, newOperationError(http.StatusUnauthorized, "Unauthorized")
	}

	session, err := s.dbQueries.GetSessionByToken(ctx, token)
	if err != nil {
		return apiPrincipal{}, newOperationError(http.StatusUnauthorized, "Unauthorized")
	}

	return apiPrincipal{
		User: AuthUser{
			ID:       session.UserID,
			Username: session.Username,
			Role:     session.Role,
		},
		SessionToken: token,
	}, nil
}

func (s *Server) authenticateBasic(ctx context.Context, username, password string) (apiPrincipal, error) {
	user, err := s.dbQueries.GetUserByUsername(ctx, username)
	if err != nil || !auth.VerifyPassword(password, user.PasswordHash) {
		return apiPrincipal{}, newOperationError(http.StatusUnauthorized, "Unauthorized")
	}

	return apiPrincipal{
		User: AuthUser{
			ID:       user.ID,
			Username: user.Username,
			Role:     user.Role,
		},
	}, nil
}

func (s *Server) recordFailedAttempt(ctx context.Context, username string) {
	attempts, err := s.dbQueries.UpsertLoginAttempt(ctx, username)
	if err != nil {
		return
	}

	if attempts.FailedCount >= maxFailedAttempts {
		lockUntil := time.Now().Add(s.lockoutDuration)
		if err := s.dbQueries.LockAccount(ctx, database.LockAccountParams{
			Username:    username,
			LockedUntil: sql.NullTime{Time: lockUntil, Valid: true},
		}); err != nil {
			log.Printf("Failed to lock account %s: %v", username, err)
		}
	}
}

func newOperationError(statusCode int, message string) *operationError {
	return &operationError{statusCode: statusCode, message: message}
}

func operationStatus(err error) (int, string) {
	var opErr *operationError
	if errors.As(err, &opErr) {
		return opErr.statusCode, opErr.message
	}

	return http.StatusInternalServerError, "Internal server error"
}

func normalizeBearerToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}

	parts := strings.SplitN(token, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return strings.TrimSpace(parts[1])
	}

	return token
}
