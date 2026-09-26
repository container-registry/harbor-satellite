package cmd

import (
	"fmt"

	"github.com/container-registry/harbor-satellite/groundctl/internal/client"
	"github.com/container-registry/harbor-satellite/groundctl/internal/output"
	"github.com/spf13/cobra"
)

func newGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "group",
		Short: "Manage image groups in Ground Control",
	}

	cmd.AddCommand(
		newGroupListCmd(),
		newGroupGetCmd(),
		newGroupAddSatelliteCmd(),
		newGroupRemoveSatelliteCmd(),
	)

	return cmd
}

func newGroupListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all image groups",
		RunE: func(cmd *cobra.Command, args []string) error {
			gc := clientFromContext(cmd)
			format := output.Format(cmd.Root().PersistentFlags().Lookup("output").Value.String())

			groups, err := gc.ListGroups(cmd.Context())
			if err != nil {
				return fmt.Errorf("list groups: %w", err)
			}
			output.PrintGroups(groups, format)
			return nil
		},
	}
}

func newGroupGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get details of a specific group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gc := clientFromContext(cmd)
			format := output.Format(cmd.Root().PersistentFlags().Lookup("output").Value.String())

			group, err := gc.GetGroup(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("get group %q: %w", args[0], err)
			}
			output.PrintGroups([]client.Group{*group}, format)
			return nil
		},
	}
}

func newGroupAddSatelliteCmd() *cobra.Command {
	var groupName string

	cmd := &cobra.Command{
		Use:   "add-satellite <satellite>",
		Short: "Add a satellite to a group",
		Args:  cobra.ExactArgs(1),
		Example: `  groundctl group add-satellite edge-tokyo-01 --group ml-models`,
		RunE: func(cmd *cobra.Command, args []string) error {
			gc := clientFromContext(cmd)

			if err := gc.AddSatelliteToGroup(cmd.Context(), args[0], groupName); err != nil {
				return fmt.Errorf("add satellite %q to group %q: %w", args[0], groupName, err)
			}
			fmt.Printf("Satellite %q added to group %q.\n", args[0], groupName)
			return nil
		},
	}

	cmd.Flags().StringVar(&groupName, "group", "", "Name of the group (required)")
	_ = cmd.MarkFlagRequired("group")
	return cmd
}

func newGroupRemoveSatelliteCmd() *cobra.Command {
	var groupName string

	cmd := &cobra.Command{
		Use:   "remove-satellite <satellite>",
		Short: "Remove a satellite from a group",
		Args:  cobra.ExactArgs(1),
		Example: `  groundctl group remove-satellite edge-tokyo-01 --group ml-models`,
		RunE: func(cmd *cobra.Command, args []string) error {
			gc := clientFromContext(cmd)

			if err := gc.RemoveSatelliteFromGroup(cmd.Context(), args[0], groupName); err != nil {
				return fmt.Errorf("remove satellite %q from group %q: %w", args[0], groupName, err)
			}
			fmt.Printf("Satellite %q removed from group %q.\n", args[0], groupName)
			return nil
		},
	}

	cmd.Flags().StringVar(&groupName, "group", "", "Name of the group (required)")
	_ = cmd.MarkFlagRequired("group")
	return cmd
}
