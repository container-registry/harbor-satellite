package cmd

import (
	"fmt"

	"github.com/container-registry/harbor-satellite/ground-control/cmd/groundctl/internal/client"
	"github.com/container-registry/harbor-satellite/ground-control/cmd/groundctl/internal/output"
	"github.com/spf13/cobra"
)

func newSatelliteCmd(apiClient *client.Client) *cobra.Command {
	satelliteCmd := &cobra.Command{
		Use:     "satellite",
		Aliases: []string{"satellites", "sat"},
		Short:   "Manage satellites",
		Long:    `Manage satellites registered with the Ground Control server.`,
	}

	satelliteCmd.AddCommand(newSatelliteListCmd(apiClient))
	satelliteCmd.AddCommand(newSatelliteInspectCmd(apiClient))

	return satelliteCmd
}

func newSatelliteListCmd(apiClient *client.Client) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all satellites",
		Long:    `List all satellites registered with the Ground Control server.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			satellites, err := apiClient.ListSatellites()
			if err != nil {
				return fmt.Errorf("list satellites: %w", err)
			}

			if len(satellites) == 0 {
				fmt.Println("No satellites found.")
				return nil
			}

			headers := []string{"ID", "NAME", "LAST SEEN", "HEARTBEAT"}
			rows := make([][]string, 0, len(satellites))

			for _, sat := range satellites {
				lastSeen := "-"
				if sat.LastSeen != nil {
					lastSeen = sat.LastSeen.Format("2006-01-02 15:04:05")
				}

				heartbeat := "-"
				if sat.HeartbeatInterval != nil {
					heartbeat = *sat.HeartbeatInterval
				}

				rows = append(rows, []string{
					fmt.Sprintf("%d", sat.ID),
					sat.Name,
					lastSeen,
					heartbeat,
				})
			}

			output.PrintTable(headers, rows)
			return nil
		},
	}
}

func newSatelliteInspectCmd(apiClient *client.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <name>",
		Short: "Show detailed information about a satellite",
		Long:  `Show detailed information about a satellite by name.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]

			sat, err := apiClient.GetSatellite(name)
			if err != nil {
				return fmt.Errorf("inspect satellite %q: %w", name, err)
			}

			lastSeen := "-"
			if sat.LastSeen != nil {
				lastSeen = sat.LastSeen.Format("2006-01-02 15:04:05")
			}

			heartbeat := "-"
			if sat.HeartbeatInterval != nil {
				heartbeat = *sat.HeartbeatInterval
			}

			rows := [][]string{
				{"ID:", fmt.Sprintf("%d", sat.ID)},
				{"Name:", sat.Name},
				{"Created:", sat.CreatedAt.Format("2006-01-02 15:04:05")},
				{"Updated:", sat.UpdatedAt.Format("2006-01-02 15:04:05")},
				{"Last Seen:", lastSeen},
				{"Heartbeat:", heartbeat},
			}

			output.PrintKeyValue(rows)
			return nil
		},
	}
}
