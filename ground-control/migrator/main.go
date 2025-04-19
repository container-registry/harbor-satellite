package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Function to run a shell command and return the output or error
func runCommand(command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// waitForPostgres waits for PostgreSQL to be ready by checking the pg_isready status
func waitForPostgres(dbHost, dbPort string) error {
	for {
		fmt.Printf("Waiting for PostgreSQL at %s:%s to be ready...\n", dbHost, dbPort)
		err := runCommand("pg_isready", "-h", dbHost, "-p", dbPort)
		if err == nil {
			fmt.Println("PostgreSQL is ready.")
			return nil
		}
		// Sleep for a while before trying again
		time.Sleep(10 * time.Second)
	}
}

// Check if the database exists
func checkDatabaseExists(dbName, dbUser, dbPassword string) (bool, error) {
	// Set the environment variable for the PostgreSQL password
	cmd := exec.Command("psql", "-h", "postgres", "-U", dbUser, "-d", "postgres", "-tc", fmt.Sprintf("SELECT 1 FROM pg_database WHERE datname = '%s'", dbName))
	cmd.Env = append(os.Environ(), "PGPASSWORD="+dbPassword)

	// Run the command and capture the output
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("error checking database: %s", err)
	}

	// Check if the output is "1" (indicating the database exists)
	if string(output) == "1\n" {
		return true, nil
	}

	return false, nil
}

// Create a new database if it doesn't exist
func createDatabase(dbName, dbUser, dbPassword string) error {
	fmt.Printf("Database %s does not exist, creating it...\n", dbName)
	cmd := exec.Command("psql", "-h", "postgres", "-U", dbUser, "-d", "postgres", "-c", fmt.Sprintf("CREATE DATABASE %s", dbName))
	cmd.Env = append(os.Environ(), "PGPASSWORD="+dbPassword)
	return cmd.Run()
}

// Run Goose migrations
func runMigrations(dbName, dbUser, dbPassword, dbHost, dbPort string) error {
	gooseCmd := fmt.Sprintf("postgres://%s:%s@%s:%s/%s", dbUser, dbPassword, dbHost, dbPort, dbName)
	cmd := exec.Command("goose", "postgres", gooseCmd, "up")
	fmt.Println(cmd)
	return cmd.Run()
}

func main() {
	// Get environment variables (DB settings)
	dbPassword := os.Getenv("DB_PASSWORD")
	dbUser := os.Getenv("DB_USERNAME")
	dbName := os.Getenv("DB_DATABASE")
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")

	if dbPassword == "" || dbUser == "" || dbName == "" || dbHost == "" || dbPort == "" {
		fmt.Println("Missing required environment variables (DB_PASSWORD, DB_USERNAME, DB_DATABASE, DB_HOST, DB_PORT).")
		os.Exit(1)
	}

	// Wait for PostgreSQL to be ready
	if err := waitForPostgres(dbHost, dbPort); err != nil {
		fmt.Println("Error waiting for PostgreSQL:", err)
		os.Exit(1)
	}

	// // Check if the database exists
	// exists, err := checkDatabaseExists(dbName, dbUser, dbPassword)
	// if err != nil {
	// 	fmt.Println("Error checking if database exists:", err)
	// 	os.Exit(1)
	// }
	//
	// // If the database doesn't exist, create it
	// if !exists {
	// 	if err := createDatabase(dbName, dbUser, dbPassword); err != nil {
	// 		fmt.Println("Error creating database:", err)
	// 		os.Exit(1)
	// 	}
	// }

	// Run Goose migrations
	if err := runMigrations(dbName, dbUser, dbPassword, dbHost, dbPort); err != nil {
		fmt.Println("Error running migrations:", err)
		os.Exit(1)
	}

	fmt.Println("Migrations completed successfully.")
}
