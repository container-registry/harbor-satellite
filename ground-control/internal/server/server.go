package server

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	_ "github.com/joho/godotenv/autoload"
	_ "github.com/lib/pq"

	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
)

type Server struct {
	port      string
	db        *sql.DB
	dbQueries *database.Queries
}

func NewServer() *http.Server {
	port := os.Getenv("PORT")
	if port == "" {
		log.Fatal("PORT is required")
	}

	_, err := strconv.Atoi(port)
	if err != nil {
		log.Fatalf("invalid PORT: %s", port)
	}

	connStr := os.Getenv("DB_URL")
	if connStr == "" {
		log.Fatal("DB_URL is required")
	}

	_, err = url.Parse(connStr)
	if err != nil {
		log.Fatalf("invalid DB_URL: %v", err)
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Error in sql: %v", err)
	}

	dbQueries := database.New(db)

	NewServer := &Server{
		port:      port,
		db:        db,
		dbQueries: dbQueries,
	}

	// Declare Server config
	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", NewServer.port),
		Handler:      NewServer.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return server
}
