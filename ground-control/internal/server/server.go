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

	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
	_ "github.com/joho/godotenv/autoload"
	_ "github.com/lib/pq"

	"container-registry.com/harbor-satellite/ground-control/internal/database"
	"container-registry.com/harbor-satellite/ground-control/reg/harbor"
)

type Server struct {
	port      int
	db        *sql.DB
	dbQueries *database.Queries
}

var (
	dbName       = os.Getenv("DB_DATABASE")
	password     = os.Getenv("DB_PASSWORD")
	username     = os.Getenv("DB_USERNAME")
	PORT         = os.Getenv("DB_PORT")
	HOST         = os.Getenv("DB_HOST")
	syncInterval = os.Getenv("SYNC_INTERVAL_SECONDS")
)

func NewServer() *http.Server {
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

	NewServer := &Server{
		port:      port,
		db:        db,
		dbQueries: dbQueries,
	}

	// Declare Server config
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", NewServer.port),
		Handler:      NewServer.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Start a background goroutine to send periodic requests every 5 seconds
	go func() {
		log.Println("executing periodic update")
		interval, err := strconv.ParseInt(syncInterval, 10, 64)
		if err != nil {
			log.Fatalf("error in getting interval from env")
		}
		ticker := time.NewTicker(time.Duration(interval) * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			hclient, err := harbor.GetClient()
			if err != nil {
				log.Printf("error getting client: %v", err)
			}

			rpolicies, err := harbor.ListReplication(context.Background(), harbor.ListParams{}, hclient)
			for _, policy := range rpolicies {
				err := UpdateGroup(policy, dbQueries)
				if err != nil {
					log.Printf("error in updating group: %v", err)
					continue
				}
			}
			// Log success
			log.Printf("Periodic request completed")
		}
	}()

	return server
}

func UpdateGroup(policy *models.ReplicationPolicy, dbQueries *database.Queries) error {
	projects, err := convertFiltersToProjects(policy.Filters)
	if err != nil {
		err = fmt.Errorf("Error in converting filters to projects: %v", err)
		return err
	}
	if policy.DestRegistry.Type == "artifact-list-export" {
		_, err := dbQueries.CreateGroup(context.Background(), database.CreateGroupParams{
			GroupName:   policy.Name,
			RegistryUrl: policy.DestRegistry.URL,
			Projects:    projects,
		})
		if err != nil {
			err = fmt.Errorf("Error in periodic request: %v", err)
			return err
		}
	}

	return nil
}

func convertFiltersToProjects(filters []*models.ReplicationFilter) ([]string, error) {
	var projects []string
	if len(filters) < 1 {
		_, err := harbor.GetClient()
		if err != nil {
			err = fmt.Errorf("Error in periodic request: %v", err)
			return nil, err
		}
		// harbor.RobotAccountTemplate()
		return projects, nil
	}

	return projects, nil
}

func UpdateRobots(projects []string) error {
	// to-do: update robot with permissions
	return nil
}
