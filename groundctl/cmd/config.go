package cmd

import (
	"fmt"

	"github.com/container-registry/harbor-satellite/groundctl/internal/client"
	"github.com/container-registry/harbor-satellite/groundctl/internal/output"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage satellite configurations in Ground Control",
	}

	cmd.AddCommand(
		newConfigListCmd(),
		newConfigGetCmd(),
		newConfigDeleteCmd(),
	)

	return cmd
}

func newConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all satellite configurations",
		RunE: func(cmd *cobra.Command, args []string) error {
			gc := clientFromContext(cmd)
			format := output.Format(cmd.Root().PersistentFlags().Lookup("output").Value.String())

			configs, err := gc.ListConfigs(cmd.Context())
			if err != nil {
				return fmt.Errorf("list configs: %w", err)
			}
			output.PrintConfigs(configs, format)
			return nil
		},
	}
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get details of a specific configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gc := clientFromContext(cmd)
			format := output.Format(cmd.Root().PersistentFlags().Lookup("output").Value.String())

			cfg, err := gc.GetConfig(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("get config %q: %w", args[0], err)
			}
			output.PrintConfigs([]client.Config{*cfg}, format)
			return nil
		},
	}
}

func newConfigDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a satellite configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gc := clientFromContext(cmd)

			if err := gc.DeleteConfig(cmd.Context(), args[0]); err != nil {
				return fmt.Errorf("delete config %q: %w", args[0], err)
			}
			fmt.Printf("Config %q deleted.\n", args[0])
			return nil
		},
	}
}
