package harbortest_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbortest"
	"github.com/goharbor/go-client/pkg/harbor"
	v2client "github.com/goharbor/go-client/pkg/sdk/v2.0/client"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/project"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/robot"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/stretchr/testify/require"
)

func client(t *testing.T, stub *harbortest.Stub) *v2client.HarborAPI {
	t.Helper()
	cs, err := harbor.NewClientSet(&harbor.ClientSetConfig{
		URL:      stub.URL(),
		Username: harbortest.Username,
		Password: harbortest.Password,
	})
	require.NoError(t, err)
	return cs.V2()
}

// checked keeps the error of a go-swagger call. Its response types implement
// error themselves, so errcheck rejects discarding them with _.
func checked[T any](_ T, err error) error {
	return err
}

func TestRobotLifecycle(t *testing.T) {
	stub := harbortest.New(t)
	api := client(t, stub)
	ctx := context.Background()

	created, err := api.Robot.CreateRobot(ctx, &robot.CreateRobotParams{Robot: &models.RobotCreate{
		Name:     "edge-01",
		Level:    "system",
		Duration: 30,
		Permissions: []*models.RobotPermission{
			{Kind: "project", Namespace: "satellite", Access: []*models.Access{{Action: "pull", Resource: "repository"}}},
		},
	}})
	require.NoError(t, err)
	require.Equal(t, "robot$edge-01", created.Payload.Name)
	require.NotEmpty(t, created.Payload.Secret)
	require.Greater(t, created.Payload.ExpiresAt, time.Now().Unix())

	q := "name=edge-01"
	listed, err := api.Robot.ListRobot(ctx, &robot.ListRobotParams{Q: &q})
	require.NoError(t, err)
	require.Len(t, listed.Payload, 1)
	require.Empty(t, listed.Payload[0].Secret)

	got, err := api.Robot.GetRobotByID(ctx, &robot.GetRobotByIDParams{RobotID: created.Payload.ID})
	require.NoError(t, err)
	got.Payload.Permissions = append(got.Payload.Permissions, &models.RobotPermission{Kind: "project", Namespace: "library"})
	require.NoError(t, checked(api.Robot.UpdateRobot(ctx, &robot.UpdateRobotParams{RobotID: created.Payload.ID, Robot: got.Payload})))
	require.Equal(t, []string{"library", "satellite"}, stub.RobotProjects("robot$edge-01"))

	refreshed, err := api.Robot.RefreshSec(ctx, &robot.RefreshSecParams{RobotID: created.Payload.ID, RobotSec: &models.RobotSec{}})
	require.NoError(t, err)
	require.NotEqual(t, created.Payload.Secret, refreshed.Payload.Secret)

	require.NoError(t, checked(api.Robot.DeleteRobot(ctx, &robot.DeleteRobotParams{RobotID: created.Payload.ID})))
	var notFound *robot.GetRobotByIDNotFound
	require.ErrorAs(t, checked(api.Robot.GetRobotByID(ctx, &robot.GetRobotByIDParams{RobotID: created.Payload.ID})), &notFound)

	require.Equal(t, map[string]int{
		harbortest.RouteCreateRobot:  1,
		harbortest.RouteListRobots:   1,
		harbortest.RouteGetRobot:     2,
		harbortest.RouteUpdateRobot:  1,
		harbortest.RouteRefreshRobot: 1,
		harbortest.RouteDeleteRobot:  1,
	}, harbortest.CountRoutes(stub.Calls()))
}

func TestAPIRequiresCredentials(t *testing.T) {
	stub := harbortest.New(t)
	cs, err := harbor.NewClientSet(&harbor.ClientSetConfig{URL: stub.URL(), Username: "admin", Password: "wrong"})
	require.NoError(t, err)

	require.Error(t, checked(cs.V2().Project.HeadProject(context.Background(), &project.HeadProjectParams{ProjectName: "satellite"})))
	require.Equal(t, http.StatusUnauthorized, stub.Calls()[0].Status)
}

func TestProjectsAndPush(t *testing.T) {
	stub := harbortest.New(t)
	api := client(t, stub)
	ctx := context.Background()

	var notFound *project.HeadProjectNotFound
	require.ErrorAs(t, checked(api.Project.HeadProject(ctx, &project.HeadProjectParams{ProjectName: "satellite"})), &notFound)

	img, err := crane.Image(map[string][]byte{"artifacts.json": []byte(`{"group":"g"}`)})
	require.NoError(t, err)
	ref := stub.Host() + "/satellite/group-state/g/state"
	require.Error(t, crane.Push(img, ref, crane.Insecure), "pushing into a missing project must fail like Harbor")

	public := true
	require.NoError(t, checked(api.Project.CreateProject(ctx, &project.CreateProjectParams{Project: &models.ProjectReq{ProjectName: "satellite", Public: &public}})))
	require.NoError(t, checked(api.Project.HeadProject(ctx, &project.HeadProjectParams{ProjectName: "satellite"})))

	require.NoError(t, crane.Push(img, ref, crane.Insecure))
	require.NoError(t, crane.Tag(ref, "latest", crane.Insecure))
	require.True(t, stub.HasRepository("satellite/group-state/g/state"))

	before := len(stub.Calls())
	content, err := stub.ArtifactFile("satellite/group-state/g/state", "latest", "artifacts.json")
	require.NoError(t, err)
	require.JSONEq(t, `{"group":"g"}`, string(content))
	require.Len(t, stub.Calls(), before, "reading back an artifact is not a GC call")

	pulled, err := crane.Pull(ref+":latest", crane.Insecure)
	require.NoError(t, err)
	want, err := img.Digest()
	require.NoError(t, err)
	got, err := pulled.Digest()
	require.NoError(t, err)
	require.Equal(t, want, got)

	deleteURL := stub.URL() + "/api/v2.0/projects/satellite/repositories/" + url.QueryEscape(url.QueryEscape("group-state/g/state"))
	require.Equal(t, http.StatusOK, doDelete(t, deleteURL))
	require.False(t, stub.HasRepository("satellite/group-state/g/state"))
	require.Equal(t, http.StatusNotFound, doDelete(t, deleteURL))
	require.Equal(t, 2, stub.Count(harbortest.RouteDeleteRepository))
	require.Positive(t, stub.Count(harbortest.RoutePutManifest))
}

func doDelete(t *testing.T, target string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, target, nil)
	require.NoError(t, err)
	req.SetBasicAuth(harbortest.Username, harbortest.Password)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp.StatusCode
}

func TestFaultsAndDown(t *testing.T) {
	stub := harbortest.New(t)
	api := client(t, stub)
	ctx := context.Background()
	id := stub.SeedRobot("edge-01", "satellite").ID

	params := &robot.GetRobotByIDParams{RobotID: id}

	stub.FailOn(harbortest.RouteGetRobot, http.StatusInternalServerError)
	var internal *robot.GetRobotByIDInternalServerError
	require.ErrorAs(t, checked(api.Robot.GetRobotByID(ctx, params)), &internal)

	stub.ClearFaults()
	require.NoError(t, checked(api.Robot.GetRobotByID(ctx, params)))

	stub.SetDown(true)
	require.Error(t, checked(api.Robot.GetRobotByID(ctx, params)))
	calls := stub.Calls()
	require.Zero(t, calls[len(calls)-1].Status)

	stub.SetDown(false)
	require.NoError(t, checked(api.Robot.GetRobotByID(ctx, params)))
}

func TestGateHoldsRequestsUntilAllArrive(t *testing.T) {
	stub := harbortest.New(t)
	api := client(t, stub)
	id := stub.SeedRobot("edge-01").ID
	stub.Gate(harbortest.RouteRefreshRobot, 2, 5*time.Second)

	refresh := func() error {
		return checked(api.Robot.RefreshSec(context.Background(), &robot.RefreshSecParams{RobotID: id, RobotSec: &models.RobotSec{}}))
	}

	start := time.Now()
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = refresh()
		}()
	}
	wg.Wait()
	require.NoError(t, errors.Join(errs...))
	require.Less(t, time.Since(start), 5*time.Second, "both parties arrived, so neither waited for the timeout")

	stub.Gate(harbortest.RouteRefreshRobot, 2, 200*time.Millisecond)
	start = time.Now()
	require.NoError(t, refresh())
	require.GreaterOrEqual(t, time.Since(start), 200*time.Millisecond, "a lone party waits for the timeout")
}

func TestResetForgetsEverything(t *testing.T) {
	stub := harbortest.New(t)
	stub.SeedRobot("edge-01")
	stub.AddProject("satellite")
	stub.FailOn(harbortest.RouteGetRobot, http.StatusInternalServerError)
	require.Error(t, checked(client(t, stub).Robot.GetRobotByID(context.Background(), &robot.GetRobotByIDParams{RobotID: 1})))

	stub.Reset()

	require.Empty(t, stub.Calls())
	require.False(t, stub.HasProject("satellite"))
	_, ok := stub.Robot("robot$edge-01")
	require.False(t, ok)
	require.Equal(t, int64(1001), stub.SeedRobot("edge-02").ID)
}
