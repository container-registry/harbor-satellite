package registry

import (
	"log"

	"zotregistry.dev/zot/pkg/cli/server"
)

func LaunchRegistry() (bool, error) {
	log.Println("Launching Registry")

	// Create the root command for the server
	rootCmd := server.NewServerRootCmd()

	// Set the arguments
	rootCmd.SetArgs([]string{"serve", "./registry/config.json"})

	// Execute the root command
	err := rootCmd.Execute()
	if err != nil {
		log.Fatalf("Error executing server root command: %v", err)
		return false, err
	}

	return true, err
}
