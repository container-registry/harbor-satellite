package harbor

import (
	"context"
	"fmt"

	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/robot"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"

	_ "github.com/joho/godotenv/autoload"
)

type ListParams struct {
	Page     int64
	PageSize int64
	Q        string
	Sort     string
}

func GetRobotDetails(r *robot.CreateRobotCreated) (int64, string, string) {
	id := r.Payload.ID
	name := r.Payload.Name
	secret := r.Payload.Secret

	return id, name, secret
}

func IsRobotPresent(ctx context.Context, name string) (bool, error) {
	client := GetClient()

  name = fmt.Sprintf("name=%s", name)
	response, err := client.Robot.ListRobot(
		ctx,
		&robot.ListRobotParams{
			Q: &name,
		},
	)
	if err != nil {
		return false, fmt.Errorf("error: listing robot account: %v", err)
	}

  if len(response.Payload) > 0 {
    return true, nil
  }

	return false, nil
}

func ListRobots(ctx context.Context, opts ListParams) (*robot.ListRobotOK, error) {
	client := GetClient()
	response, err := client.Robot.ListRobot(
		ctx,
		&robot.ListRobotParams{
			Page:     &opts.Page,
			PageSize: &opts.PageSize,
			Q:        &opts.Q,
			Sort:     &opts.Sort,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("error: listing robot account: %v", err)
	}
	return response, nil
}

func DeleteRobotAccount(ctx context.Context, robotID int64) (*robot.DeleteRobotOK, error) {
	client := GetClient()
	response, err := client.Robot.DeleteRobot(
		ctx,
		&robot.DeleteRobotParams{
			RobotID: robotID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("error: deleting robot account: %v", err)
	}
	return response, nil
}

func RefreshRobotAccount(ctx context.Context, secret string, robotID int64) (*robot.RefreshSecOK, error) {
	client := GetClient()
	response, err := client.Robot.RefreshSec(
		ctx,
		&robot.RefreshSecParams{
			RobotSec: &models.RobotSec{
				Secret: secret,
			},
			RobotID: robotID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("error: updating robot account: %v", err)
	}
	return response, nil
}

func UpdateRobotAccount(ctx context.Context, opts *models.Robot) (*robot.UpdateRobotOK, error) {
	client := GetClient()
	response, err := client.Robot.UpdateRobot(
		ctx,
		&robot.UpdateRobotParams{
			Robot:   opts,
			RobotID: opts.ID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("error: updating robot account: %v", err)
	}
	return response, nil
}

func GetRobotAccount(ctx context.Context, id int64) (*models.Robot, error) {
	client := GetClient()
	response, err := client.Robot.GetRobotByID(
		ctx,
		&robot.GetRobotByIDParams{
			RobotID: id,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("error: getting robot account: %v", err)
	}
	return response.Payload, nil
}

func CreateRobotAccount(ctx context.Context, opts *models.RobotCreate) (*robot.CreateRobotCreated, error) {
	client := GetClient()
	response, err := client.Robot.CreateRobot(
		ctx,
		&robot.CreateRobotParams{
			Robot: opts,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("error: create robot account in adapter: %v", err.Error())
	}
	return response, nil
}

func RobotAccountTemplate(name string, projects []string) *models.RobotCreate {
	robotPermissions := GenRobotPerms(projects)
	robotAccount := &models.RobotCreate{
		Description: "managed by ground-control should not edit",
		Disable:     false,
		Duration:    -1,
		Level:       "system",
		Name:        name,
		Permissions: robotPermissions,
	}

	return robotAccount
}

func GenRobotPerms(projects []string) []*models.RobotPermission {
	robotAccess := []*models.Access{
		{Action: "read", Resource: "artifact"},
		{Action: "read", Resource: "repository"},
		{Action: "pull", Resource: "repository"},
	}

	// permissions for the provided projects
	var robotPermissions []*models.RobotPermission
	if len(projects) > 0 {
		for _, project := range projects {
			robotPermissions = append(robotPermissions, &models.RobotPermission{
				Access:    robotAccess,
				Kind:      "project",
				Namespace: project,
			})
		}
	}
	return robotPermissions
}
