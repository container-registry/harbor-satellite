package cmd

import (
	"fmt"

	"github.com/container-registry/harbor-satellite/groundctl/internal/apply"
	"github.com/container-registry/harbor-satellite/groundctl/internal/output"
	"github.com/spf13/cobra"
)

func newApplyCmd() *cobra.Command {
	var (
		filePath string
		dryRun   bool
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply a declarative fleet manifest to Ground Control",
		Long: `Apply reads a SatelliteFleet YAML manifest and reconciles the live Ground
Control state to match it. Satellites that exist in the manifest but not in
Ground Control are registered. Satellites that exist in Ground Control but not
in the manifest are deleted. Unchanged satellites are left alone.

Use --dry-run to preview the changes without applying them.`,
		Example: `  # Apply a fleet manifest
  groundctl apply -f fleet.yaml

  # Preview changes without applying
  groundctl apply -f fleet.yaml --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if filePath == "" {
				return fmt.Errorf("flag -f/--file is required")
			}

			manifest, err := apply.ParseManifest(filePath)
			if err != nil {
				return fmt.Errorf("parse manifest: %w", err)
			}

			gc := clientFromContext(cmd)

			if dryRun {
				fmt.Println("Dry run — no changes will be applied.\n")
			}

			result, err := apply.Reconcile(cmd.Context(), gc, manifest, dryRun)
			if err != nil {
				return fmt.Errorf("reconcile fleet: %w", err)
			}

			output.PrintApplyResult(result.Created, result.Deleted, result.Updated, result.Unchanged)
			return nil
		},
	}

	cmd.Flags().StringVarP(&filePath, "file", "f", "", "Path to the fleet manifest YAML file (required)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without applying them")

	return cmd
}
