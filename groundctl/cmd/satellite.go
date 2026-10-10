package cmd

import (
	"fmt"
	"os"

	"github.com/container-registry/harbor-satellite/groundctl/internal/client"
	"github.com/container-registry/harbor-satellite/groundctl/internal/output"
	"github.com/spf13/cobra"
)

func newSatelliteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "satellite",
		Short:   "Manage satellites registered with Ground Control",
		Aliases: []string{"sat"},
	}

	cmd.AddCommand(
		newSatelliteListCmd(),
		newSatelliteGetCmd(),
		newSatelliteRegisterCmd(),
		newSatelliteDeleteCmd(),
		newSatelliteStatusCmd(),
		newSatelliteImagesCmd(),
	)

	return cmd
}

func newSatelliteListCmd() *cobra.Command {
	var (
		limit      int
		offset     int
		namePrefix string
		sort       string
		order      string
		activeOnly bool
		staleOnly  bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all registered satellites",
		Example: `  # List all satellites
  groundctl satellite list

  # Filter by name prefix with pagination
  groundctl satellite list --name edge --limit 20 --offset 0

  # Show only active satellites
  groundctl satellite list --active

  # Output as JSON
  groundctl satellite list --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			gc := clientFromContext(cmd)
			ctx := cmd.Context()
			format := output.Format(cmd.Root().PersistentFlags().Lookup("output").Value.String())

			if activeOnly {
				satellites, err := gc.GetActiveSatellites(ctx)
				if err != nil {
					return fmt.Errorf("list active satellites: %w", err)
				}
				output.PrintSatellites(satellites, format)
				return nil
			}

			if staleOnly {
				satellites, err := gc.GetStaleSatellites(ctx)
				if err != nil {
					return fmt.Errorf("list stale satellites: %w", err)
				}
				output.PrintSatellites(satellites, format)
				return nil
			}

			satellites, err := gc.ListSatellites(ctx, client.ListSatellitesParams{
				Limit:      limit,
				Offset:     offset,
				NamePrefix: namePrefix,
				Sort:       sort,
				Order:      order,
			})
			if err != nil {
				return fmt.Errorf("list satellites: %w", err)
			}
			output.PrintSatellites(satellites, format)
			return nil
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of satellites to return (0 = all)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Number of satellites to skip")
	cmd.Flags().StringVar(&namePrefix, "name", "", "Filter satellites by name prefix")
	cmd.Flags().StringVar(&sort, "sort", "name", "Sort field (name, created_at, last_seen)")
	cmd.Flags().StringVar(&order, "order", "asc", "Sort order (asc, desc)")
	cmd.Flags().BoolVar(&activeOnly, "active", false, "Show only active satellites")
	cmd.Flags().BoolVar(&staleOnly, "stale", false, "Show only stale satellites")

	return cmd
}

func newSatelliteGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "get <name>",
		Short:   "Get details of a specific satellite",
		Args:    cobra.ExactArgs(1),
		Example: `  groundctl satellite get edge-tokyo-01`,
		RunE: func(cmd *cobra.Command, args []string) error {
			gc := clientFromContext(cmd)
			format := output.Format(cmd.Root().PersistentFlags().Lookup("output").Value.String())

			sat, err := gc.GetSatellite(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("get satellite %q: %w", args[0], err)
			}
			output.PrintSatellite(sat, format)
			return nil
		},
	}
}

func newSatelliteRegisterCmd() *cobra.Command {
	var (
		configName string
		groups     []string
	)

	cmd := &cobra.Command{
		Use:   "register <name>",
		Short: "Register a new satellite with Ground Control",
		Args:  cobra.ExactArgs(1),
		Example: `  groundctl satellite register edge-tokyo-01 --config prod-config --groups ml-models,base-images`,
		RunE: func(cmd *cobra.Command, args []string) error {
			gc := clientFromContext(cmd)

			params := client.RegisterSatelliteParams{
				Name:       args[0],
				ConfigName: configName,
			}
			if len(groups) > 0 {
				params.Groups = &groups
			}

			resp, err := gc.RegisterSatellite(cmd.Context(), params)
			if err != nil {
				return fmt.Errorf("register satellite %q: %w", args[0], err)
			}

			fmt.Printf("Satellite %q registered successfully.\n", args[0])
			fmt.Printf("One-time token (use within 24h): %s\n", resp.Token)
			return nil
		},
	}

	cmd.Flags().StringVar(&configName, "config", "", "Config name to assign to the satellite (required)")
	cmd.Flags().StringSliceVar(&groups, "groups", nil, "Comma-separated list of groups to assign")
	_ = cmd.MarkFlagRequired("config")

	return cmd
}

func newSatelliteDeleteCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:     "delete <name>",
		Short:   "Delete a satellite from Ground Control",
		Args:    cobra.ExactArgs(1),
		Example: `  groundctl satellite delete edge-tokyo-01`,
		RunE: func(cmd *cobra.Command, args []string) error {
			gc := clientFromContext(cmd)

			if !force {
				fmt.Printf("Delete satellite %q? This cannot be undone. [y/N]: ", args[0])
				var confirm string
				fmt.Fscanln(os.Stdin, &confirm)
				if confirm != "y" && confirm != "Y" {
					fmt.Println("Aborted.")
					return nil
				}
			}

			if err := gc.DeleteSatellite(cmd.Context(), args[0]); err != nil {
				return fmt.Errorf("delete satellite %q: %w", args[0], err)
			}

			fmt.Printf("Satellite %q deleted.\n", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	return cmd
}

func newSatelliteStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "status <name>",
		Short:   "Show the latest sync status of a satellite",
		Args:    cobra.ExactArgs(1),
		Example: `  groundctl satellite status edge-tokyo-01`,
		RunE: func(cmd *cobra.Command, args []string) error {
			gc := clientFromContext(cmd)
			format := output.Format(cmd.Root().PersistentFlags().Lookup("output").Value.String())

			status, err := gc.GetSatelliteStatus(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("get status for satellite %q: %w", args[0], err)
			}
			output.PrintSatelliteStatus(args[0], status, format)
			return nil
		},
	}
}

func newSatelliteImagesCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "images <name>",
		Short:   "List images cached by a satellite",
		Args:    cobra.ExactArgs(1),
		Example: `  groundctl satellite images edge-tokyo-01`,
		RunE: func(cmd *cobra.Command, args []string) error {
			gc := clientFromContext(cmd)
			format := output.Format(cmd.Root().PersistentFlags().Lookup("output").Value.String())

			images, err := gc.GetSatelliteImages(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("get images for satellite %q: %w", args[0], err)
			}
			output.PrintCachedImages(args[0], images, format)
			return nil
		},
	}
}
