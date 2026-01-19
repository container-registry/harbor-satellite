package server

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/joho/godotenv/autoload"
	_ "github.com/lib/pq"

	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
)

type Server struct {
	port      int
	db        *sql.DB
	dbQueries *database.Queries
}

var (
	dbName   = os.Getenv("DB_DATABASE")
	password = os.Getenv("DB_PASSWORD")
	username = os.Getenv("DB_USERNAME")
	PORT     = os.Getenv("DB_PORT")
	HOST     = os.Getenv("DB_HOST")
)

func NewServer(ctx context.Context) *http.Server {
	port, err := strconv.Atoi(os.Getenv("PORT"))
	if err != nil {
		log.Fatalf("PORT is not valid: %v", err)
	}

	connStr := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		username,
		password,
		HOST,
		PORT,
		dbName,
	)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Error in sql: %v", err)
	}

	dbQueries := database.New(db)

	s := &Server{
		port:      port,
		db:        db,
		dbQueries: dbQueries,
	}

	// Bootstrap system admin user
	if err := s.BootstrapSystemAdmin(context.Background()); err != nil {
		log.Fatalf("Failed to bootstrap system admin: %v", err)
	}

	go s.StartCleanupJob(ctx, CleanupConfig{
		RetentionDays:   defaultRetentionDays,
		CleanupInterval: defaultCleanupInterval,
	})

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      s.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return server
}
