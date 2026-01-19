package server

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/container-registry/harbor-satellite/ground-control/internal/auth"
	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
)

const systemAdminUsername = "admin"

// BootstrapSystemAdmin creates the system admin user if it doesn't exist
func (s *Server) BootstrapSystemAdmin(ctx context.Context) error {
	exists, err := s.dbQueries.SystemAdminExists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check system admin existence: %w", err)
	}

	if exists {
		log.Println("System admin already exists, skipping bootstrap")
		return nil
	}

	password := os.Getenv("ADMIN_PASSWORD")
	if password == "" {
		return fmt.Errorf("ADMIN_PASSWORD environment variable is required for initial setup")
	}

	if len(password) < minPasswordLength {
		return fmt.Errorf("ADMIN_PASSWORD must be at least %d characters", minPasswordLength)
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("failed to hash admin password: %w", err)
	}

	_, err = s.dbQueries.CreateUser(ctx, database.CreateUserParams{
		Username:     systemAdminUsername,
		PasswordHash: hash,
		Role:         roleSystemAdmin,
	})
	if err != nil {
		return fmt.Errorf("failed to create system admin: %w", err)
	}

	log.Println("System admin user created successfully")
	return nil
}
