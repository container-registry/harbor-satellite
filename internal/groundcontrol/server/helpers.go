package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func DecodeRequestBody(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return &AppError{
			Message: "Invalid request body",
			Code:    http.StatusBadRequest,
		}
	}
	return nil
}

// creates a unique random API token of the specified length in bytes.
func GenerateRandomToken(charLength int) (string, error) {
	// The number of bytes needed to generate a token with the required number of hex characters
	byteLength := charLength / 2

	// Create a byte slice of the required length
	token := make([]byte, byteLength)
	_, err := rand.Read(token)
	if err != nil {
		return "", err
	}

	// Return the token as a hex-encoded string
	return hex.EncodeToString(token), nil
}

// maskToken returns a masked version of a token for secure logging.
func maskToken(token string) string {
	if len(token) < 8 {
		return "***"
	}
	return fmt.Sprintf("%s...%s", token[:4], token[len(token)-4:])
}

func GetAuthToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		err := &AppError{
			Message: "Authorization header missing",
			Code:    http.StatusUnauthorized,
		}
		return "", err
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		err := &AppError{
			Message: "Invalid Authorization header format",
			Code:    http.StatusUnauthorized,
		}
		return "", err
	}
	token := parts[1]

	return token, nil
}

func toNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func toNullInt64(n int64) sql.NullInt64 {
	return sql.NullInt64{Int64: n, Valid: true}
}

func toNullInt32(n int32) sql.NullInt32 {
	return sql.NullInt32{Int32: n, Valid: true}
}

// normalizeHeartbeatInterval validates and normalizes the heartbeat interval to a canonical format.
// Expected input format: "@every HHhMMmSSs" (e.g., "@every 00h01m30s")
// Returns the normalized interval string or an error if the format is invalid.
func normalizeHeartbeatInterval(interval string) (string, error) {
	if interval == "" {
		return "", nil
	}

	const prefix = "@every "
	if !strings.HasPrefix(interval, prefix) {
		return "", fmt.Errorf("invalid heartbeat interval format: must start with %q", prefix)
	}

	durationStr := strings.TrimPrefix(interval, prefix)
	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return "", fmt.Errorf("invalid heartbeat interval duration %q: %w", durationStr, err)
	}

	if duration <= 0 {
		return "", fmt.Errorf("heartbeat interval must be positive, got %v", duration)
	}

	if duration < time.Second {
		return "", fmt.Errorf("heartbeat interval must be at least 1 second, got %v", duration)
	}

	if duration.Nanoseconds()%int64(time.Second) != 0 {
		return "", fmt.Errorf("heartbeat interval must be a whole number of seconds, got %v", duration)
	}

	// Normalize to canonical format: HHhMMmSSs
	hours := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60
	seconds := int(duration.Seconds()) % 60

	return fmt.Sprintf("@every %02dh%02dm%02ds", hours, minutes, seconds), nil
}
