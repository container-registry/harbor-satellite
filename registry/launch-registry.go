package registry

import (
	"zotregistry.dev/zot/pkg/cli/server"
)

func LaunchRegistry(zotConfigPath string) (bool, error) {

	// Create the root command for the server
	rootCmd := server.NewServerRootCmd()

	// Set the arguments
	rootCmd.SetArgs([]string{"serve", zotConfigPath})

	// Execute the root command
	err := rootCmd.Execute()
	if err != nil {
		return false, err
	}

	return true, err
}
