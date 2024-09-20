package harbor

import (
	"context"
	"fmt"

	v2client "github.com/goharbor/go-client/pkg/sdk/v2.0/client"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/robot"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"

	_ "github.com/joho/godotenv/autoload"
)

type ListRobotParams struct {
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

func ListRobots(ctx context.Context, opts ListRobotParams, client *v2client.HarborAPI) (*robot.ListRobotOK, error) {
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

func DeleteRobotAccount(ctx context.Context, robotID int64, client *v2client.HarborAPI) (*robot.DeleteRobotOK, error) {
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

func RefreshRobotAccount(ctx context.Context, secret string, robotID int64, client *v2client.HarborAPI) (*robot.RefreshSecOK, error) {
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

func UpdateRobotAccount(ctx context.Context, opts *models.Robot, client *v2client.HarborAPI) (*robot.UpdateRobotOK, error) {
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

func CreateRobotAccount(ctx context.Context, opts *models.RobotCreate, client *v2client.HarborAPI) (*robot.CreateRobotCreated, error) {
	response, err := client.Robot.CreateRobot(
		ctx,
		&robot.CreateRobotParams{
			Robot: opts,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("error: creating robot account: %v", err)
	}
	return response, nil
}

func RobotAccountTemplate(name string, projects []string) *models.RobotCreate {
	robotAccess := []*models.Access{
		{Action: "read", Resource: "artifact"},
		{Action: "read", Resource: "repository"},
		{Action: "pull", Resource: "repository"},
	}

	// permissions for the provided projects
	var robotPermissions []*models.RobotPermission
	for _, project := range projects {
		robotPermissions = append(robotPermissions, &models.RobotPermission{
			Access:    robotAccess,
			Kind:      "project",
			Namespace: project,
		})
	}

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
