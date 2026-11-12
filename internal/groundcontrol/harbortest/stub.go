// Package harbortest is an in-process stand-in for the Harbor API and
// registry endpoints that Ground Control calls. It keeps just enough state
// (robots, projects, repositories, pushed images) for GC's handlers to run,
// and records every request so tests can assert on the calls GC made.
package harbortest

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
)

// Credentials the stub accepts on the Harbor API.
const (
	Username = "admin"
	Password = "Harbor12345"
)

// RobotPrefix is what Harbor prepends to system robot names.
const RobotPrefix = "robot$"

// Route keys identify the endpoint a Call hit, independent of IDs and names.
const (
	RouteHealth                  = "GET /api/v2.0/health"
	RouteListRobots              = "GET /api/v2.0/robots"
	RouteCreateRobot             = "POST /api/v2.0/robots"
	RouteGetRobot                = "GET /api/v2.0/robots/{id}"
	RouteUpdateRobot             = "PUT /api/v2.0/robots/{id}"
	RouteRefreshRobot            = "PATCH /api/v2.0/robots/{id}"
	RouteDeleteRobot             = "DELETE /api/v2.0/robots/{id}"
	RouteHeadProject             = "HEAD /api/v2.0/projects"
	RouteCreateProject           = "POST /api/v2.0/projects"
	RouteListReplicationPolicies = "GET /api/v2.0/replication/policies"
	RouteDeleteRepository        = "DELETE /api/v2.0/projects/{project}/repositories/{repository}"
	RoutePutManifest             = "PUT /v2/{name}/manifests/{reference}"
)

const apiPrefix = "/api/v2.0"

// Call is one request the stub received.
type Call struct {
	Method string
	// Path is the decoded request path.
	Path  string
	Query string
	Route string
	// Body is kept for API calls and manifests; blob uploads are dropped.
	Body []byte
	// Status is 0 when the stub was down and closed the connection.
	Status int
}

// Registry reports whether the call went to the OCI distribution API.
func (c Call) Registry() bool {
	return strings.HasPrefix(c.Path, "/v2")
}

// Stub is a fake Harbor. All methods are safe for concurrent use.
type Stub struct {
	server *httptest.Server

	mu           sync.Mutex
	registry     http.Handler
	calls        []Call
	robots       map[int64]*models.Robot
	nextRobotID  int64
	secretSerial int
	projects     map[string]bool
	repositories map[string]bool
	policies     []string
	faults       map[string]int
	gates        map[string]*gate
	down         bool
}

// Start runs a stub until Close is called. Use New inside a test.
func Start() *Stub {
	s := &Stub{}
	s.resetLocked()
	s.server = httptest.NewServer(http.HandlerFunc(s.serveHTTP))
	return s
}

// New runs a stub for the duration of the test.
func New(t testing.TB) *Stub {
	t.Helper()
	s := Start()
	t.Cleanup(s.Close)
	return s
}

func (s *Stub) Close() {
	s.server.Close()
}

// URL is the base URL to use as HARBOR_URL.
func (s *Stub) URL() string {
	return s.server.URL
}

// Host is URL without the scheme, as it appears in image references.
func (s *Stub) Host() string {
	return strings.TrimPrefix(s.server.URL, "http://")
}

// Reset forgets all state, recorded calls, faults and gates.
func (s *Stub) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, g := range s.gates {
		g.release()
	}
	s.resetLocked()
}

func (s *Stub) resetLocked() {
	s.registry = registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	s.calls = nil
	s.robots = map[int64]*models.Robot{}
	s.nextRobotID = 1000
	s.secretSerial = 0
	s.projects = map[string]bool{}
	s.repositories = map[string]bool{}
	s.policies = nil
	s.faults = map[string]int{}
	s.gates = map[string]*gate{}
	s.down = false
}

// SetDown makes the stub close every connection without answering.
func (s *Stub) SetDown(down bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.down = down
}

// FailOn answers every request on route with status until ClearFaults.
func (s *Stub) FailOn(route string, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults[route] = status
}

func (s *Stub) ClearFaults() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults = map[string]int{}
}

// Gate holds requests on route until parties of them are waiting or the
// timeout passes, then lets all of them through. It forces concurrent
// requests to overlap at a known point.
func (s *Stub) Gate(route string, parties int, timeout time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gates[route] = &gate{parties: parties, timeout: timeout, open: make(chan struct{})}
}

// AddProject creates a project as if it already existed in Harbor.
func (s *Stub) AddProject(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projects[name] = true
}

func (s *Stub) HasProject(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.projects[name]
}

// AddReplicationPolicy makes the policy list return a policy with this name.
func (s *Stub) AddReplicationPolicy(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policies = append(s.policies, name)
}

// HasRepository reports whether a manifest was pushed to repository and the
// repository was not deleted since.
func (s *Stub) HasRepository(repository string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.repositories[repository]
}

// ArtifactFile reads a file from the image pushed to repository:tag, such as
// "artifacts.json" of a state artifact. The read is not recorded as a call.
func (s *Stub) ArtifactFile(repository, tag, file string) ([]byte, error) {
	s.mu.Lock()
	transport := handlerTransport{s.registry}
	s.mu.Unlock()
	img, err := crane.Pull(s.Host()+"/"+repository+":"+tag, crane.Insecure, crane.WithTransport(transport))
	if err != nil {
		return nil, err
	}
	rc := mutate.Extract(img)
	defer func() { _ = rc.Close() }()
	tr := tar.NewReader(rc)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s not found in %s:%s", file, repository, tag)
		}
		if err != nil {
			return nil, err
		}
		if hdr.Name == file {
			return io.ReadAll(tr)
		}
	}
}

// handlerTransport serves requests straight from a handler, bypassing the
// recording server.
type handlerTransport struct {
	h http.Handler
}

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, r)
	return rec.Result(), nil
}

// SeedRobot creates a robot as if GC had created it earlier and returns it
// including its secret.
func (s *Stub) SeedRobot(name string, projects ...string) models.Robot {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.createRobotLocked(&models.RobotCreate{
		Name:        name,
		Level:       "system",
		Duration:    30,
		Permissions: robotPermissions(projects),
	})
	return *r
}

// Robot returns the robot with the full name, for example "robot$edge-01".
func (s *Stub) Robot(name string) (models.Robot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.robots {
		if r.Name == name {
			return *r, true
		}
	}
	return models.Robot{}, false
}

// RobotProjects returns the sorted project namespaces the robot may access.
func (s *Stub) RobotProjects(name string) []string {
	r, ok := s.Robot(name)
	if !ok {
		return nil
	}
	return PermissionNamespaces(r.Permissions)
}

// Calls returns a copy of every recorded call in completion order.
func (s *Stub) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

// Mark returns a position to pass to CallsSince.
func (s *Stub) Mark() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *Stub) CallsSince(mark int) []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mark > len(s.calls) {
		return nil
	}
	return append([]Call(nil), s.calls[mark:]...)
}

// Count returns how many recorded calls hit route.
func (s *Stub) Count(route string) int {
	return CountRoutes(s.Calls())[route]
}

// CountRoutes tallies calls per route key.
func CountRoutes(calls []Call) map[string]int {
	out := map[string]int{}
	for _, c := range calls {
		out[c.Route]++
	}
	return out
}

// PermissionNamespaces returns the sorted, de-duplicated namespaces.
func PermissionNamespaces(perms []*models.RobotPermission) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range perms {
		if p == nil || seen[p.Namespace] {
			continue
		}
		seen[p.Namespace] = true
		out = append(out, p.Namespace)
	}
	sort.Strings(out)
	return out
}

func (s *Stub) serveHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	call := Call{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Route: routeOf(r.Method, r.URL.Path)}
	if !call.Registry() || call.Route == RoutePutManifest {
		call.Body = body
	}

	s.mu.Lock()
	down := s.down
	fault, faulted := s.faults[call.Route]
	g := s.gates[call.Route]
	s.mu.Unlock()

	if down {
		s.record(call)
		closeConnection(w)
		return
	}
	if g != nil {
		g.wait()
	}

	rec := &statusRecorder{ResponseWriter: w}
	switch {
	case faulted:
		writeError(rec, fault, "injected fault")
	case strings.HasPrefix(r.URL.Path, apiPrefix+"/"):
		s.serveAPI(rec, r, call.Route, body)
	case call.Registry():
		s.serveRegistry(rec, r, call.Route)
	default:
		writeError(rec, http.StatusNotFound, "not served by harbortest")
	}
	call.Status = rec.status()
	s.record(call)
}

func (s *Stub) record(c Call) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, c)
}

func closeConnection(w http.ResponseWriter) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		panic("harbortest: response writer cannot be hijacked")
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		panic(fmt.Sprintf("harbortest: hijack: %v", err))
	}
	_ = conn.Close()
}

func routeOf(method, path string) string {
	if strings.HasPrefix(path, "/v2") {
		return method + " " + registryRoute(path)
	}
	if !strings.HasPrefix(path, apiPrefix+"/") {
		return method + " " + path
	}
	rest := strings.TrimPrefix(path, apiPrefix)
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	switch {
	case len(parts) == 2 && parts[0] == "robots":
		return method + " " + apiPrefix + "/robots/{id}"
	case len(parts) == 4 && parts[0] == "projects" && parts[2] == "repositories":
		return method + " " + apiPrefix + "/projects/{project}/repositories/{repository}"
	default:
		return method + " " + path
	}
}

func registryRoute(path string) string {
	rest := strings.TrimPrefix(path, "/v2/")
	switch {
	case path == "/v2/" || path == "/v2":
		return "/v2/"
	case strings.Contains(rest, "/manifests/"):
		return "/v2/{name}/manifests/{reference}"
	case strings.HasSuffix(rest, "/blobs/uploads/"):
		return "/v2/{name}/blobs/uploads/"
	case strings.Contains(rest, "/blobs/uploads/"):
		return "/v2/{name}/blobs/uploads/{uuid}"
	case strings.Contains(rest, "/blobs/"):
		return "/v2/{name}/blobs/{digest}"
	case strings.HasSuffix(rest, "/tags/list"):
		return "/v2/{name}/tags/list"
	default:
		return path
	}
}

func (s *Stub) serveAPI(w http.ResponseWriter, r *http.Request, route string, body []byte) {
	if route == RouteHealth {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":     "healthy",
			"components": []map[string]string{{"name": "core", "status": "healthy"}, {"name": "registry", "status": "healthy"}},
		})
		return
	}
	if user, pass, ok := r.BasicAuth(); !ok || user != Username || pass != Password {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	switch route {
	case RouteListRobots:
		s.listRobots(w, r)
	case RouteCreateRobot:
		s.createRobot(w, body)
	case RouteGetRobot, RouteUpdateRobot, RouteRefreshRobot, RouteDeleteRobot:
		s.robotByID(w, r, body)
	case RouteHeadProject:
		if s.projects[r.URL.Query().Get("project_name")] {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	case RouteCreateProject:
		s.createProject(w, body)
	case RouteListReplicationPolicies:
		s.listPolicies(w, r)
	case RouteDeleteRepository:
		s.deleteRepository(w, r)
	default:
		writeError(w, http.StatusNotFound, "not served by harbortest")
	}
}

// queryValue extracts key from Harbor's q syntax, "k1=v1,k2=v2".
func queryValue(r *http.Request, key string) string {
	for _, kv := range strings.Split(r.URL.Query().Get("q"), ",") {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v
		}
	}
	return ""
}

func (s *Stub) listRobots(w http.ResponseWriter, r *http.Request) {
	name := queryValue(r, "name")
	out := []*models.Robot{}
	for _, id := range s.robotIDsLocked() {
		robot := s.robots[id]
		if name == "" || robot.Name == RobotPrefix+name {
			out = append(out, withoutSecret(robot))
		}
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(len(out)))
	writeJSON(w, http.StatusOK, out)
}

func (s *Stub) createRobot(w http.ResponseWriter, body []byte) {
	var req models.RobotCreate
	if err := json.Unmarshal(body, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid robot")
		return
	}
	for _, r := range s.robots {
		if r.Name == RobotPrefix+req.Name {
			writeError(w, http.StatusConflict, "robot already exists")
			return
		}
	}
	robot := s.createRobotLocked(&req)
	w.Header().Set("Location", fmt.Sprintf("%s/robots/%d", apiPrefix, robot.ID))
	writeJSON(w, http.StatusCreated, models.RobotCreated{
		ID:           robot.ID,
		Name:         robot.Name,
		Secret:       robot.Secret,
		CreationTime: robot.CreationTime,
		ExpiresAt:    robot.ExpiresAt,
	})
}

func (s *Stub) createRobotLocked(req *models.RobotCreate) *models.Robot {
	s.nextRobotID++
	duration := req.Duration
	now := time.Now().UTC()
	robot := &models.Robot{
		ID:           s.nextRobotID,
		Name:         RobotPrefix + req.Name,
		Description:  req.Description,
		Level:        req.Level,
		Disable:      req.Disable,
		Duration:     &duration,
		Editable:     true,
		Permissions:  req.Permissions,
		Secret:       s.newSecretLocked(s.nextRobotID),
		CreationTime: strfmt.DateTime(now),
		UpdateTime:   strfmt.DateTime(now),
		ExpiresAt:    expiresAt(now, duration),
	}
	s.robots[robot.ID] = robot
	return robot
}

func (s *Stub) robotByID(w http.ResponseWriter, r *http.Request, body []byte) {
	id, err := strconv.ParseInt(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid robot id")
		return
	}
	robot, ok := s.robots[id]
	if !ok {
		writeError(w, http.StatusNotFound, "robot not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, withoutSecret(robot))
	case http.MethodPut:
		var update models.Robot
		if err := json.Unmarshal(body, &update); err != nil {
			writeError(w, http.StatusBadRequest, "invalid robot")
			return
		}
		robot.Permissions = update.Permissions
		robot.Description = update.Description
		robot.Disable = update.Disable
		robot.UpdateTime = strfmt.DateTime(time.Now().UTC())
		w.WriteHeader(http.StatusOK)
	case http.MethodPatch:
		var sec models.RobotSec
		if err := json.Unmarshal(body, &sec); err != nil {
			writeError(w, http.StatusBadRequest, "invalid secret")
			return
		}
		robot.Secret = sec.Secret
		if robot.Secret == "" {
			robot.Secret = s.newSecretLocked(robot.ID)
		}
		now := time.Now().UTC()
		robot.UpdateTime = strfmt.DateTime(now)
		if robot.Duration != nil {
			robot.ExpiresAt = expiresAt(now, *robot.Duration)
		}
		writeJSON(w, http.StatusOK, models.RobotSec{Secret: robot.Secret})
	case http.MethodDelete:
		delete(s.robots, id)
		w.WriteHeader(http.StatusOK)
	}
}

func (s *Stub) createProject(w http.ResponseWriter, body []byte) {
	var req models.ProjectReq
	if err := json.Unmarshal(body, &req); err != nil || req.ProjectName == "" {
		writeError(w, http.StatusBadRequest, "invalid project")
		return
	}
	if s.projects[req.ProjectName] {
		writeError(w, http.StatusConflict, "project already exists")
		return
	}
	s.projects[req.ProjectName] = true
	w.Header().Set("Location", apiPrefix+"/projects/"+req.ProjectName)
	w.WriteHeader(http.StatusCreated)
}

func (s *Stub) listPolicies(w http.ResponseWriter, r *http.Request) {
	name := queryValue(r, "name")
	out := []*models.ReplicationPolicy{}
	for i, p := range s.policies {
		if name == "" || p == name {
			out = append(out, &models.ReplicationPolicy{ID: int64(i + 1), Name: p})
		}
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(len(out)))
	writeJSON(w, http.StatusOK, out)
}

// deleteRepository mirrors Harbor, which expects the repository name inside
// the project to be URL-encoded twice.
func (s *Stub) deleteRepository(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, apiPrefix+"/projects/"), "/")
	repo, err := url.PathUnescape(parts[2])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repository name")
		return
	}
	full := parts[0] + "/" + repo
	if !s.repositories[full] {
		writeError(w, http.StatusNotFound, "repository "+full+" not found")
		return
	}
	delete(s.repositories, full)
	w.WriteHeader(http.StatusOK)
}

func (s *Stub) serveRegistry(w *statusRecorder, r *http.Request, route string) {
	name := repositoryName(r.URL.Path)
	write := r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch
	s.mu.Lock()
	reg := s.registry
	projectMissing := write && name != "" && !s.projects[strings.SplitN(name, "/", 2)[0]]
	s.mu.Unlock()
	if projectMissing {
		writeRegistryError(w, http.StatusNotFound, "NAME_UNKNOWN", "project of "+name+" not found")
		return
	}

	reg.ServeHTTP(w, r)

	if route == RoutePutManifest && w.status() == http.StatusCreated {
		s.mu.Lock()
		s.repositories[name] = true
		s.mu.Unlock()
	}
}

func repositoryName(path string) string {
	rest := strings.TrimPrefix(path, "/v2/")
	for _, marker := range []string{"/manifests/", "/blobs/", "/tags/"} {
		if i := strings.Index(rest, marker); i >= 0 {
			return rest[:i]
		}
	}
	return ""
}

func (s *Stub) robotIDsLocked() []int64 {
	ids := make([]int64, 0, len(s.robots))
	for id := range s.robots {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func (s *Stub) newSecretLocked(robotID int64) string {
	s.secretSerial++
	return fmt.Sprintf("stub-secret-%d-%d", robotID, s.secretSerial)
}

func withoutSecret(r *models.Robot) *models.Robot {
	c := *r
	c.Secret = ""
	return &c
}

func expiresAt(now time.Time, durationDays int64) int64 {
	if durationDays <= 0 {
		return -1
	}
	return now.Add(time.Duration(durationDays) * 24 * time.Hour).Unix()
}

func robotPermissions(projects []string) []*models.RobotPermission {
	perms := make([]*models.RobotPermission, 0, len(projects))
	for _, p := range projects {
		perms = append(perms, &models.RobotPermission{
			Kind:      "project",
			Namespace: p,
			Access:    []*models.Access{{Action: "pull", Resource: "repository"}},
		})
	}
	return perms
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) //nolint:errcheck // The client sees a truncated body; nothing to do here.
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, models.Errors{Errors: []*models.Error{{Code: http.StatusText(status), Message: msg}}})
}

func writeRegistryError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"errors": []map[string]string{{"code": code, "message": msg}}})
}

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.code == 0 {
		r.code = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.code == 0 {
		r.code = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) status() int {
	if r.code == 0 {
		return http.StatusOK
	}
	return r.code
}

type gate struct {
	parties int
	timeout time.Duration

	mu      sync.Mutex
	arrived int
	open    chan struct{}
}

func (g *gate) wait() {
	g.mu.Lock()
	g.arrived++
	if g.arrived >= g.parties {
		g.releaseLocked()
	}
	g.mu.Unlock()

	timer := time.NewTimer(g.timeout)
	defer timer.Stop()
	select {
	case <-g.open:
	case <-timer.C:
	}
}

func (g *gate) release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.releaseLocked()
}

func (g *gate) releaseLocked() {
	select {
	case <-g.open:
	default:
		close(g.open)
	}
}
