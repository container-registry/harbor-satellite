// Package harbor2 implements fleet.Registry over the Harbor v2 API and
// crane (plan item 1.3). Each Registry builds its own client from Config:
// no process-wide client, no env reads.
package harbor2

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	httptransport "github.com/go-openapi/runtime/client"
	v2client "github.com/goharbor/go-client/pkg/sdk/v2.0/client"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/health"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/project"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/replication"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/client/robot"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
)

var _ fleet.Registry = (*Registry)(nil)

type Config struct {
	URL, Username, Password string
	// Insecure talks plain HTTP to the registry, as an http:// URL does
	// today.
	Insecure bool
	// HTTPClient is used for the API and the registry; nil is a default
	// client.
	HTTPClient *http.Client
}

type Registry struct {
	cfg  Config
	api  *v2client.HarborAPI
	http *http.Client
}

const robotDescription = "managed by ground-control should not edit"

func New(cfg Config) (*Registry, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("%w: registry URL %q", fleet.ErrInvalid, cfg.URL)
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = v2client.DefaultBasePath
	}
	api := v2client.New(v2client.Config{URL: u, Transport: transport, AuthInfo: httptransport.BasicAuth(cfg.Username, cfg.Password)})
	return &Registry{cfg: cfg, api: api, http: client}, nil
}

// mapError turns client errors into the fleet sentinels: no answer or a
// gateway status is ErrRegistryUnavailable, 404 ErrNotFound and 409
// ErrAlreadyExists.
func mapError(op string, err error) error {
	if err == nil {
		return nil
	}
	var coded interface{ IsCode(int) bool }
	if errors.As(err, &coded) {
		switch {
		case coded.IsCode(http.StatusNotFound):
			return fmt.Errorf("%s: %w: %w", op, fleet.ErrNotFound, err)
		case coded.IsCode(http.StatusConflict):
			return fmt.Errorf("%s: %w: %w", op, fleet.ErrAlreadyExists, err)
		case coded.IsCode(http.StatusBadGateway), coded.IsCode(http.StatusServiceUnavailable), coded.IsCode(http.StatusGatewayTimeout):
			return fmt.Errorf("%s: %w: %w", op, fleet.ErrRegistryUnavailable, err)
		}
		return fmt.Errorf("%s: %w", op, err)
	}
	var urlErr *url.Error
	var netErr net.Error
	if errors.As(err, &urlErr) || errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w: %w", op, fleet.ErrRegistryUnavailable, err)
	}
	return fmt.Errorf("%s: %w", op, err)
}

// answered is mapError for calls whose success response the client also
// types as an error.
func answered(op string, resp interface{ IsSuccess() bool }, err error) error {
	if err != nil {
		return mapError(op, err)
	}
	if resp == nil || !resp.IsSuccess() {
		return fmt.Errorf("%s: no success answer", op)
	}
	return nil
}

func (r *Registry) FindRobot(ctx context.Context, name string) (fleet.RobotCredentials, error) {
	q := "name=" + name
	resp, err := r.api.Robot.ListRobot(ctx, &robot.ListRobotParams{Q: &q})
	if err != nil {
		return fleet.RobotCredentials{}, mapError("find robot "+name, err)
	}
	for _, rb := range resp.Payload {
		if rb.Name == name || strings.HasSuffix(rb.Name, "$"+name) {
			return fleet.RobotCredentials{HarborID: rb.ID, Name: rb.Name, ExpiresAt: unixTime(rb.ExpiresAt)}, nil
		}
	}
	return fleet.RobotCredentials{}, fmt.Errorf("robot %s: %w", name, fleet.ErrNotFound)
}

func unixTime(s int64) time.Time {
	if s <= 0 {
		return time.Time{}
	}
	return time.Unix(s, 0)
}

func permissions(projects []string) []*models.RobotPermission {
	access := []*models.Access{
		{Action: "read", Resource: "artifact"},
		{Action: "read", Resource: "repository"},
		{Action: "pull", Resource: "repository"},
	}
	out := make([]*models.RobotPermission, 0, len(projects))
	for _, p := range projects {
		out = append(out, &models.RobotPermission{Access: access, Kind: "project", Namespace: p})
	}
	return out
}

// days rounds d up to whole days, at least one, because Harbor counts
// robot lifetimes in days.
func days(d time.Duration) int64 {
	const day = 24 * time.Hour
	n := int64((d + day - 1) / day)
	return max(n, 1)
}

// CreateRobot creates a system robot that may read and pull the spec's
// projects, as harbor.RobotAccountTemplate does, for spec.Duration rounded
// up to whole days because Harbor counts in days.
func (r *Registry) CreateRobot(ctx context.Context, spec fleet.RobotSpec) (fleet.RobotCredentials, error) {
	resp, err := r.api.Robot.CreateRobot(ctx, &robot.CreateRobotParams{Robot: &models.RobotCreate{
		Description: robotDescription,
		Duration:    days(spec.Duration),
		Level:       "system",
		Name:        spec.Name,
		Permissions: permissions(spec.Projects),
	}})
	if err != nil {
		return fleet.RobotCredentials{}, mapError("create robot "+spec.Name, err)
	}
	p := resp.Payload
	return fleet.RobotCredentials{HarborID: p.ID, Name: p.Name, Secret: p.Secret, ExpiresAt: unixTime(p.ExpiresAt)}, nil
}

// SetRobotAccess reads the robot and writes it back with the new
// permissions and disable flag in one PUT, because Harbor's update
// replaces the whole robot; every other field is written back as read.
func (r *Registry) SetRobotAccess(ctx context.Context, harborID int64, projects []string, disabled bool) error {
	got, err := r.api.Robot.GetRobotByID(ctx, &robot.GetRobotByIDParams{RobotID: harborID})
	if err != nil {
		return mapError("get robot "+strconv.FormatInt(harborID, 10), err)
	}
	rb := got.Payload
	sorted := slices.Clone(projects)
	slices.Sort(sorted)
	rb.Permissions = permissions(slices.Compact(sorted))
	rb.Disable = disabled
	resp, err := r.api.Robot.UpdateRobot(ctx, &robot.UpdateRobotParams{Robot: rb, RobotID: harborID})
	return answered("update robot "+strconv.FormatInt(harborID, 10), resp, err)
}

// RotateRobotSecret lets Harbor generate a new secret and reads the robot
// back for its name and its extended expiry, which the refresh answer
// leaves out.
func (r *Registry) RotateRobotSecret(ctx context.Context, harborID int64) (fleet.RobotCredentials, error) {
	id := strconv.FormatInt(harborID, 10)
	resp, err := r.api.Robot.RefreshSec(ctx, &robot.RefreshSecParams{RobotID: harborID, RobotSec: &models.RobotSec{}})
	if err != nil {
		return fleet.RobotCredentials{}, mapError("refresh robot "+id, err)
	}
	creds := fleet.RobotCredentials{HarborID: harborID, Secret: resp.Payload.Secret}
	got, err := r.api.Robot.GetRobotByID(ctx, &robot.GetRobotByIDParams{RobotID: harborID})
	if err != nil {
		return fleet.RobotCredentials{}, mapError("get robot "+id, err)
	}
	creds.Name = got.Payload.Name
	creds.ExpiresAt = unixTime(got.Payload.ExpiresAt)
	return creds, nil
}

func (r *Registry) DeleteRobot(ctx context.Context, harborID int64) error {
	resp, err := r.api.Robot.DeleteRobot(ctx, &robot.DeleteRobotParams{RobotID: harborID})
	return answered("delete robot "+strconv.FormatInt(harborID, 10), resp, err)
}

func (r *Registry) craneOptions(ctx context.Context) []crane.Option {
	opts := []crane.Option{
		crane.WithAuth(authn.FromConfig(authn.AuthConfig{Username: r.cfg.Username, Password: r.cfg.Password})),
		crane.WithContext(ctx),
	}
	if r.cfg.HTTPClient != nil && r.cfg.HTTPClient.Transport != nil {
		opts = append(opts, crane.WithTransport(r.cfg.HTTPClient.Transport))
	}
	if r.cfg.Insecure || strings.HasPrefix(r.cfg.URL, "http://") {
		opts = append(opts, crane.Insecure)
	}
	return opts
}

func registryHost(raw string) string {
	return strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://"), "/")
}

// PushState stores payload as artifacts.json at ref, tags it with the push
// time and latest and returns the manifest digest.
func (r *Registry) PushState(ctx context.Context, ref fleet.ArtifactRef, payload []byte) (fleet.Digest, error) {
	img, err := crane.Image(map[string][]byte{"artifacts.json": payload})
	if err != nil {
		return "", fmt.Errorf("build state image: %w", err)
	}
	digest, err := img.Digest()
	if err != nil {
		return "", fmt.Errorf("digest state image: %w", err)
	}
	repo := registryHost(ref.Registry) + "/" + ref.Repository
	opts := r.craneOptions(ctx)
	if err := crane.Push(img, repo, opts...); err != nil {
		return "", mapError("push state "+ref.String(), err)
	}
	tags := []string{strconv.FormatInt(time.Now().Unix(), 10), ref.Tag}
	for _, tag := range tags {
		if err := crane.Tag(repo, tag, opts...); err != nil {
			return "", mapError("tag state "+repo+":"+tag, err)
		}
	}
	return fleet.Digest(digest.String()), nil
}

// DeleteState deletes the state repository through the Harbor API, which
// expects the repository name URL-encoded twice.
func (r *Registry) DeleteState(ctx context.Context, ref fleet.ArtifactRef) error {
	projectName, repository, ok := strings.Cut(ref.Repository, "/")
	if !ok {
		return fmt.Errorf("%w: state repository %q has no project", fleet.ErrInvalid, ref.Repository)
	}
	target := strings.TrimSuffix(r.cfg.URL, "/") + "/api/v2.0/projects/" + url.PathEscape(projectName) +
		"/repositories/" + url.QueryEscape(url.QueryEscape(repository))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, target, nil)
	if err != nil {
		return fmt.Errorf("build state delete request: %w", err)
	}
	req.SetBasicAuth(r.cfg.Username, r.cfg.Password)
	resp, err := r.http.Do(req)
	if err != nil {
		return mapError("delete state "+ref.String(), err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("state %s: %w", ref, fleet.ErrNotFound)
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return fmt.Errorf("delete state %s: status %d: %w", ref, resp.StatusCode, fleet.ErrRegistryUnavailable)
	default:
		return fmt.Errorf("delete state %s: status %d", ref, resp.StatusCode)
	}
}

func (r *Registry) EnsureProject(ctx context.Context, name string) error {
	head, err := r.api.Project.HeadProject(ctx, &project.HeadProjectParams{ProjectName: name})
	if err == nil && head.IsSuccess() {
		return nil
	}
	if err = mapError("head project "+name, err); !errors.Is(err, fleet.ErrNotFound) {
		return err
	}
	public, storage := true, int64(-1)
	created, err := r.api.Project.CreateProject(ctx, &project.CreateProjectParams{Project: &models.ProjectReq{
		ProjectName: name, Public: &public, StorageLimit: &storage,
	}})
	if err = answered("create project "+name, created, err); errors.Is(err, fleet.ErrAlreadyExists) {
		return nil
	}
	return err
}

func (r *Registry) ReplicationPolicyExists(ctx context.Context, name string) (bool, error) {
	q := "name=" + name
	resp, err := r.api.Replication.ListReplicationPolicies(ctx, &replication.ListReplicationPoliciesParams{Q: &q})
	if err != nil {
		return false, mapError("list replication policies", err)
	}
	for _, p := range resp.Payload {
		if p.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (r *Registry) Ping(ctx context.Context) error {
	resp, err := r.api.Health.GetHealth(ctx, &health.GetHealthParams{})
	return answered("registry health", resp, err)
}
