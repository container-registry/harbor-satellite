package harbor

import (
	"context"
	"fmt"

	v2client "github.com/goharbor/go-client/pkg/sdk/v2.0/client"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/project"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
)

func ListProjects(ctx context.Context, opts *models.RobotCreate, client *v2client.HarborAPI) ([]string, error) {
	n := int64(1000)
	response, err := client.Project.ListProjects(
		ctx,
		&project.ListProjectsParams{
			PageSize: &n,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("error: listing projects: %v", err)
	}

  var projects []string

  for _, project := range response.Payload {
    projects = append(projects, project.Name)
  }
	return projects, nil
}
