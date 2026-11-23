package harbor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"

	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/project"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
)

const satelliteProjectName = "satellite"

type projectAPI interface {
	HeadProject(context.Context, *project.HeadProjectParams) (*project.HeadProjectOK, error)
	CreateProject(context.Context, *project.CreateProjectParams) (*project.CreateProjectCreated, error)
	GetProject(context.Context, *project.GetProjectParams) (*project.GetProjectOK, error)
	UpdateProject(context.Context, *project.UpdateProjectParams) (*project.UpdateProjectOK, error)
}

// EnsureSatelliteProject creates the satellite project as private. An existing
// public satellite project is made private.
func EnsureSatelliteProject(ctx context.Context) error {
	return ensureSatelliteProject(ctx, GetClient().Project)
}

func ensureSatelliteProject(ctx context.Context, api projectAPI) error {
	exists, err := projectExists(ctx, api, satelliteProjectName)
	if err != nil {
		return err
	}
	if !exists {
		return createSatelliteProject(ctx, api)
	}
	return makeSatelliteProjectPrivate(ctx, api)
}

func projectExists(ctx context.Context, api projectAPI, name string) (bool, error) {
	proj, err := api.HeadProject(ctx, &project.HeadProjectParams{
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

func createSatelliteProject(ctx context.Context, api projectAPI) error {
	public := false
	storage := int64(-1)
	log.Println("creating project satellite")
	proj, err := api.CreateProject(ctx, &project.CreateProjectParams{
		Project: &models.ProjectReq{
			ProjectName: satelliteProjectName,
			Public:      &public,
			Metadata: &models.ProjectMetadata{
				Public: "false",
			},
			StorageLimit: &storage,
		},
	})
	if err != nil {
		return fmt.Errorf("error: project create request failed for project satellite: %w", err)
	}
	if proj == nil || !proj.IsSuccess() {
		return fmt.Errorf("error: project create request failed for project satellite")
	}
	return nil
}

func makeSatelliteProjectPrivate(ctx context.Context, api projectAPI) error {
	got, err := api.GetProject(ctx, &project.GetProjectParams{
		ProjectNameOrID: satelliteProjectName,
		XIsResourceName: namedResource(),
	})
	if err != nil {
		return fmt.Errorf("error: project get request failed for project %s: %w", satelliteProjectName, err)
	}
	if got == nil || got.Payload == nil {
		return fmt.Errorf("error: project get request failed for project %s", satelliteProjectName)
	}
	if !metadataIsPublic(got.Payload.Metadata) {
		return nil
	}

	log.Println("setting project satellite to private")
	updated, err := api.UpdateProject(ctx, &project.UpdateProjectParams{
		ProjectNameOrID: satelliteProjectName,
		XIsResourceName: namedResource(),
		Project: &models.ProjectReq{
			Metadata: &models.ProjectMetadata{Public: "false"},
		},
	})
	if err != nil {
		return fmt.Errorf("error: project update request failed for project %s: %w", satelliteProjectName, err)
	}
	if updated == nil || !updated.IsSuccess() {
		return fmt.Errorf("error: project update request failed for project %s", satelliteProjectName)
	}
	return nil
}

func metadataIsPublic(meta *models.ProjectMetadata) bool {
	if meta == nil || meta.Public == "" {
		return false
	}
	public, err := strconv.ParseBool(meta.Public)
	return err == nil && public
}

func namedResource() *bool {
	named := true
	return &named
}
