package harbor

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/project"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
)

func GetProject(ctx context.Context, name string) (bool, error) {
	client, err := GetClient()
	if err != nil {
		return false, fmt.Errorf("getting harbor client: %w", err)
	}
	proj, err := client.Project.HeadProject(ctx, &project.HeadProjectParams{
		ProjectName: name,
	})
	if err != nil {
		var notFound *project.HeadProjectNotFound
		if errors.As(err, &notFound) {
			return false, nil
		}
		return false, fmt.Errorf("error: project head request failed for project %s: %w", name, err)
	}
	return proj.IsSuccess(), nil
}

func CreateSatelliteProject(ctx context.Context) (bool, error) {
	client, err := GetClient()
	if err != nil {
		return false, fmt.Errorf("getting harbor client: %w", err)
	}
	var (
		public  = true
		storage = int64(-1)
	)
	log.Println("creating project satellite")
	proj, err := client.Project.CreateProject(ctx, &project.CreateProjectParams{
		Project: &models.ProjectReq{
			ProjectName:  "satellite",
			Public:       &public,
			StorageLimit: &storage,
		},
	})
	if err != nil {
		return false, fmt.Errorf("error: project create request failed for project satellite: %w", err)
	}
	return proj.IsSuccess(), nil
}
