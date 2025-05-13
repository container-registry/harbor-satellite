package robot

import (
	"fmt"
	"os"

	"github.com/container-registry/harbor-satellite/ground-control/reg/harbor"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/robot"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
	"github.com/spf13/cobra"
)

func NewCreateRobotCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-gc-robot",
		Short: "Create a robot account for Ground Control operations",
		Long: `Create a robot account in Harbor with appropriate permissions for Ground Control operations.
This follows security best practices by using a dedicated robot account instead of admin credentials.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get from env
			harborURL := os.Getenv("HARBOR_URL")
			harborUsername := os.Getenv("HARBOR_USERNAME")
			harborPassword := os.Getenv("HARBOR_PASSWORD")

			if harborURL == "" || harborUsername == "" || harborPassword == "" {
				return fmt.Errorf("HARBOR_URL, HARBOR_USERNAME, and HARBOR_PASSWORD must be set")
			}

			// Create a client
			client := harbor.GetClient()
			if client == nil {
				return fmt.Errorf("failed to create Harbor client")
			}

			// Create robot account with system level permissions
			robotReq := &models.RobotCreate{
				Name:        "ground-control-robot",
				Description: "Robot account for Ground Control operations",
				Level:       "system",
				Disable:     false,
				Duration:    -1, // Never expires for now
				Permissions: []*models.RobotPermission{
					{
						Kind:      "system",
						Namespace: "/",
						Access: []*models.Access{
							{Resource: "project", Action: "create"},
							{Resource: "project", Action: "read"},
							{Resource: "project", Action: "update"},
							{Resource: "repository", Action: "pull"},
							{Resource: "repository", Action: "push"},
							{Resource: "artifact", Action: "read"},
							{Resource: "artifact", Action: "list"},
						},
					},
				},
			}

			// We create the robot
			created, err := client.Robot.CreateRobot(
				robot.NewCreateRobotParams().WithRobot(robotReq),
				nil,
			)

			if err != nil {
				return fmt.Errorf("failed to create robot account: %v", err)
			}

			robotAccount := created.Payload

			robotGet, err := client.Robot.GetRobotByID(
				robot.NewGetRobotByIDParams().WithRobotID(robotAccount.ID),
				nil,
			)
			if err != nil {
				return fmt.Errorf("failed to verify robot account: %v", err)
			}

			if robotGet.Payload.ID != robotAccount.ID {
				return fmt.Errorf("verification failed: robot account ID mismatch")
			}

			fmt.Println("✅ Robot account created and verified successfully!")
			fmt.Printf("Robot ID: %d\n", robotAccount.ID)
			fmt.Printf("Robot Name: %s\n", robotAccount.Name)
			fmt.Printf("Robot Secret: %s\n", robotAccount.Secret)
			fmt.Println()
			fmt.Println("To use these credentials with Ground Control, set these environment variables:")
			fmt.Printf("export HARBOR_USERNAME=%s\n", robotAccount.Name)
			fmt.Printf("export HARBOR_PASSWORD=%s\n", robotAccount.Secret)
			fmt.Printf("export HARBOR_URL=%s\n", harborURL)

			return nil
		},
	}

	return cmd

}
