package root

import (
	"fmt"
	"runtime"

	"github.com/container-registry/harbor-satellite/ground-control/cmd/gcctl/version"
	"github.com/spf13/cobra"
)

// VersionCommand returns the "version" cobra command.
// It displays the CLI version, git commit, build date, and Go/OS info.
func VersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Short:   "Show gcctl version information",
		Example: "  gcctl version",
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("gcctl version %s\n", version.Version)
			fmt.Printf("  Git Commit:  %s\n", version.GitCommit)
			fmt.Printf("  Build Date:  %s\n", version.BuildDate)
			fmt.Printf("  Go Version:  %s\n", runtime.Version())
			fmt.Printf("  OS/Arch:     %s/%s\n", runtime.GOOS, runtime.GOARCH)
		},
	}
}
