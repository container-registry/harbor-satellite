package harbor

import (
	"context"
	"errors"
	"testing"

	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/project"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
	"github.com/stretchr/testify/require"
)

func TestEnsureSatelliteProjectCreatesPrivate(t *testing.T) {
	api := &fakeProjects{exists: false}

	err := ensureSatelliteProject(context.Background(), api)

	require.NoError(t, err)
	require.NotNil(t, api.created)
	require.NotNil(t, api.created.Project)
	require.Equal(t, satelliteProjectName, api.created.Project.ProjectName)
	require.NotNil(t, api.created.Project.Public)
	require.False(t, *api.created.Project.Public)
	require.NotNil(t, api.created.Project.Metadata)
	require.Equal(t, "false", api.created.Project.Metadata.Public)
	require.Nil(t, api.updated)
}

func TestEnsureSatelliteProjectClosesExistingPublicProject(t *testing.T) {
	api := &fakeProjects{
		exists: true,
		project: &models.Project{
			Metadata: &models.ProjectMetadata{Public: "true"},
		},
	}

	err := ensureSatelliteProject(context.Background(), api)

	require.NoError(t, err)
	require.Nil(t, api.created)
	require.NotNil(t, api.updated)
	require.Equal(t, satelliteProjectName, api.updated.ProjectNameOrID)
	require.NotNil(t, api.updated.Project)
	require.NotNil(t, api.updated.Project.Metadata)
	require.Equal(t, "false", api.updated.Project.Metadata.Public)
	require.Nil(t, api.updated.Project.Public)
	require.Nil(t, api.updated.Project.StorageLimit)
}

func TestEnsureSatelliteProjectLeavesPrivateProjectAlone(t *testing.T) {
	api := &fakeProjects{
		exists: true,
		project: &models.Project{
			Metadata: &models.ProjectMetadata{
				Public:   "false",
				AutoScan: strPtr("true"),
			},
		},
	}

	err := ensureSatelliteProject(context.Background(), api)

	require.NoError(t, err)
	require.Nil(t, api.created)
	require.Nil(t, api.updated)
}

func TestEnsureSatelliteProjectTreatsMissingMetadataAsPrivate(t *testing.T) {
	api := &fakeProjects{exists: true, project: &models.Project{}}

	err := ensureSatelliteProject(context.Background(), api)

	require.NoError(t, err)
	require.Nil(t, api.updated)
}

func TestEnsureSatelliteProjectReturnsHeadError(t *testing.T) {
	api := &fakeProjects{headErr: errors.New("harbor down")}

	err := ensureSatelliteProject(context.Background(), api)

	require.Error(t, err)
	require.Nil(t, api.created)
	require.Nil(t, api.updated)
}

func TestEnsureSatelliteProjectReturnsUpdateError(t *testing.T) {
	api := &fakeProjects{
		exists: true,
		project: &models.Project{
			Metadata: &models.ProjectMetadata{Public: "true"},
		},
		updateErr: errors.New("forbidden"),
	}

	err := ensureSatelliteProject(context.Background(), api)

	require.Error(t, err)
}

type fakeProjects struct {
	exists    bool
	headErr   error
	project   *models.Project
	getErr    error
	updateErr error
	created   *project.CreateProjectParams
	updated   *project.UpdateProjectParams
}

func (f *fakeProjects) HeadProject(context.Context, *project.HeadProjectParams) (*project.HeadProjectOK, error) {
	if f.headErr != nil {
		return nil, f.headErr
	}
	if !f.exists {
		return nil, &project.HeadProjectNotFound{}
	}
	return project.NewHeadProjectOK(), nil
}

func (f *fakeProjects) CreateProject(_ context.Context, params *project.CreateProjectParams) (*project.CreateProjectCreated, error) {
	f.created = params
	return project.NewCreateProjectCreated(), nil
}

func (f *fakeProjects) GetProject(context.Context, *project.GetProjectParams) (*project.GetProjectOK, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return &project.GetProjectOK{Payload: f.project}, nil
}

func (f *fakeProjects) UpdateProject(_ context.Context, params *project.UpdateProjectParams) (*project.UpdateProjectOK, error) {
	f.updated = params
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return project.NewUpdateProjectOK(), nil
}

func strPtr(v string) *string {
	return &v
}
