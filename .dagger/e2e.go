package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"dagger/harbor-satellite/internal/dagger"
)

const (
	harborDomain  = "http://core:8080"
	harborAPIPath = "/api/v2.0"
	harborBaseURL = harborDomain + harborAPIPath

	harborAdminUser     = "admin"
	harborAdminPassword = "Harbor12345"

	harborImageTag   = "satellite"
	postgresImage    = "registry.goharbor.io/dockerhub/goharbor/harbor-db:dev" 
	redisImage       = "registry.goharbor.io/dockerhub/goharbor/redis-photon:dev"
	registryImage    = "registry.goharbor.io/harbor-next/harbor-registry:" + harborImageTag
	registryCtlImage = "registry.goharbor.io/harbor-next/harbor-registryctl:" + harborImageTag
	coreImage        = "registry.goharbor.io/harbor-next/harbor-core:" + harborImageTag
	jobImage         = "registry.goharbor.io/harbor-next/harbor-jobservice:" + harborImageTag

	configDirPath = "./test/e2e/testconfig/config/"

	postgresPort  = 5432
	redisPort     = 6379
	registryPort  = 5000
	corePort      = 8080
	coreDebugPort = 4001

	projectName       = "edge"
	registryName      = "test-registry"
	replicationPolicy = "satellite-group"
	destNamespace     = "test-group"
	policyId          = 1
)

// Test end to end satellite flow
func (m *HarborSatellite) TestEndToEnd(ctx context.Context) (string, error) {
	m.startPostgres(ctx)
	m.startGroundControl(ctx)
	m.SetupHarborRegistry(ctx)
	m.registerSatelliteAndZTR(ctx)
	return m.getImageFromZot(ctx)
}

func (m *HarborSatellite) startPostgres(ctx context.Context) {
	_, err := dag.Container().
		From("postgres:17@sha256:6cf6142afacfa89fb28b894d6391c7dcbf6523c33178bdc33e782b3b533a9342").
		WithEnvVariable("POSTGRES_USER", "postgres").
		WithEnvVariable("POSTGRES_PASSWORD", "password").
		WithEnvVariable("POSTGRES_DB", "groundcontrol").
		WithExposedPort(5432).
		AsService().WithHostname("postgres").Start(ctx)

	if err != nil {
		log.Fatalf("Failed to start PostgreSQL service: %v", err)
	}
}

func (m *HarborSatellite) startGroundControl(ctx context.Context) {

	gcDir := m.Source.Directory("./ground-control")

	_, err := dag.Container().
		From("golang:1.24-alpine@sha256:68932fa6d4d4059845c8f40ad7e654e626f3ebd3706eef7846f319293ab5cb7a").
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod")).
		WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
		WithMountedCache("/go/build-cache", dag.CacheVolume("go-build")).
		WithEnvVariable("GOCACHE", "/go/build-cache").
		WithDirectory("/app", gcDir).
		WithWorkdir("/app").
		WithEnvVariable("DB_HOST", "postgres").
		WithEnvVariable("DB_PORT", "5432").
		WithEnvVariable("DB_USERNAME", "postgres").
		WithEnvVariable("DB_PASSWORD", "password").
		WithEnvVariable("DB_DATABASE", "groundcontrol").
		WithEnvVariable("PORT", "8080").
		WithEnvVariable("HARBOR_USERNAME", harborAdminUser).
		WithEnvVariable("HARBOR_PASSWORD", harborAdminPassword).
		WithEnvVariable("HARBOR_URL", harborDomain).
		WithExec([]string{"go", "install", "github.com/pressly/goose/v3/cmd/goose@latest"}).
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithWorkdir("/app/sql/schema").
		WithExec([]string{"goose", "postgres",
			"postgres://postgres:password@postgres:5432/groundcontrol?sslmode=disable", "up"}).
		WithWorkdir("/app").
		WithExec([]string{"go", "build", "-o", "ground-control", "main.go"}).
		WithExposedPort(8080, dagger.ContainerWithExposedPortOpts{ExperimentalSkipHealthcheck: true}).
		WithEntrypoint([]string{"./ground-control"}).
		AsService().WithHostname("gc").Start(ctx)

	if err != nil {
		log.Fatalf("failed to start ground control service: %v", err)
	}

	checkHealthGroundControl(ctx)

}

func (m *HarborSatellite) startPostgresql(ctx context.Context) error {
	_, err := dag.Container().
		From(postgresImage).
		WithExposedPort(postgresPort).
		AsService().
		WithHostname("postgresql").
		Start(ctx)

	return err
}

func (m *HarborSatellite) startRedis(ctx context.Context) error {
	_, err := dag.Container().
		From(redisImage).
		WithExposedPort(redisPort).
		AsService().
		WithHostname("redis").
		Start(ctx)

	return err
}

func (m *HarborSatellite) startRegistry(ctx context.Context) error {

	regConfigDir := m.Source.Directory(configDirPath + "registry")

	_, err := dag.Container().
		From(registryImage).
		WithMountedDirectory("/etc/registry", regConfigDir).
		WithExposedPort(registryPort).
		WithoutExposedPort(5001).
		WithoutExposedPort(5443).
		AsService().
		WithHostname("registry").
		Start(ctx)

	return err
}

func (m *HarborSatellite) startRegistryCtl(ctx context.Context) error {

	regConfigDir := m.Source.Directory(configDirPath + "registry")
	regCtlConfig := m.Source.File(configDirPath + "registryctl/config.yml")
	envFile := m.Source.File(configDirPath + "jobservice/env")
	runScript := m.Source.File(configDirPath + "run_env.sh")

	_, err := dag.Container().
		From(registryCtlImage).
		WithMountedDirectory("/etc/registry", regConfigDir).
		WithMountedFile("/etc/registryctl/config.yml", regCtlConfig).
		WithMountedFile("/envFile", envFile).
		WithMountedFile("/run_script", runScript).
		WithExec([]string{"chmod", "+x", "/run_script"}).
		WithEntrypoint([]string{"/run_script", "/registryctl -c /etc/registryctl/config.yml"}).
		AsService().
		WithHostname("registryctl").
		Start(ctx)

	return err
}

func (m *HarborSatellite) startCore(ctx context.Context) (*dagger.Service, error) {

	coreConfig := m.Source.File(configDirPath + "core/app.conf")
	envCoreFile := m.Source.File(configDirPath + "core/env")
	runDebug := m.Source.File(configDirPath + "run_debug.sh")
	privatekey := m.Source.File(configDirPath + "core/private_key.pem")

	return dag.Container().
		From(coreImage).
		WithMountedFile("/etc/core/app.conf", coreConfig).
		WithMountedFile("/etc/core/private_key.pem", privatekey).
		WithMountedFile("/envFile", envCoreFile).
		WithMountedFile("/run_script", runDebug).
		WithExec([]string{"chmod", "+x", "/run_script"}).
		WithExposedPort(corePort, dagger.ContainerWithExposedPortOpts{ExperimentalSkipHealthcheck: true}).
		WithExposedPort(coreDebugPort, dagger.ContainerWithExposedPortOpts{ExperimentalSkipHealthcheck: true}).
		WithEntrypoint([]string{"/run_script", "/core", fmt.Sprintf("%d", coreDebugPort)}).
		AsService().
		WithHostname("core").
		Start(ctx)

}

func (m *HarborSatellite) startJobService(ctx context.Context) error {

	jobSrvConfig := m.Source.File(configDirPath + "jobservice/config.yml")
	envFile := m.Source.File(configDirPath + "jobservice/env")
	run_script := m.Source.File(configDirPath + "run_env.sh")

	_, err := dag.Container().
		From(jobImage).
		WithMountedFile("/etc/jobservice/config.yml", jobSrvConfig).
		WithMountedDirectory("/var/log/jobs", m.Source.Directory(configDirPath+"jobservice")).
		WithMountedFile("/envFile", envFile).
		WithMountedFile("/run_script", run_script).
		WithExec([]string{"chmod", "+x", "/run_script"}).
		WithExposedPort(8080).
		WithEntrypoint([]string{"/run_script", "/jobservice -c /etc/jobservice/config.yml"}).
		AsService().
		WithHostname("jobservice").
		Start(ctx)

	return err
}

func requireNoExecError(err error, step string) {
	var e *dagger.ExecError
	if errors.As(err, &e) {
		log.Fatalf("failed to %s (exec error): %s", step, err)
	} else {
		log.Fatalf("failed to %s (unexpected error): %s", step, err)
	}
}

// Setup harbor registry for creating groups to replicate container image at edge
func (m *HarborSatellite) SetupHarborRegistry(ctx context.Context) {
	log.Println("setting up harbor registry environment...")

	if err := m.startPostgresql(ctx); err != nil {
		requireNoExecError(err, "start postgresql")
	}
	log.Println("postgresql service started")

	if err := m.startRedis(ctx); err != nil {
		requireNoExecError(err, "start redis")
	}
	log.Println("redis service started")

	if err := m.startRegistry(ctx); err != nil {
		requireNoExecError(err, "start registry")
	}
	log.Println("registry service started")

	if err := m.startRegistryCtl(ctx); err != nil {
		requireNoExecError(err, "start registryctl")
	}
	log.Println("registryctl service started")

	_, err := m.startCore(ctx)
	if err != nil {
		requireNoExecError(err, "start core service")
	}
	log.Println("core service started")

	if err := waitForCoreServiceHealth(ctx); err != nil {
		requireNoExecError(err, "core service health check")
	}
	log.Println("core service health check passed")

	if err := m.startJobService(ctx); err != nil {
		requireNoExecError(err, "start job service")
	}
	log.Println("job service started")

	if err := initializeHarborRegistry(ctx); err != nil {
		requireNoExecError(err, "initialize harbor registry")
	}

	log.Println("harbor registry setup completed successfully")
}

func waitForCoreServiceHealth(ctx context.Context) error {
	timeout := time.After(15 * time.Minute)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for services to be healthy")
		case <-ticker.C:
			err := executeHTTPRequest(ctx, "GET", "/health", "")
			if err == nil {
				log.Println("core service is healthy")
				return nil
			}
			log.Printf("Services not ready yet: %v", err)
		}
	}
}

func initializeHarborRegistry(ctx context.Context) error {
	log.Println("initializing harbor registry...")

	requests := []func(ctx context.Context) error{
		createProject,
		listProjects,
		pushToRegistry,
		listArtifacts,
		listAdapters,
		pingRegistry,
		createRegistry,
		listRegistries,
		createConfig,
		createReplicationPolicy,
		executeReplication,
		getExecuteReplication,
	}

	for _, request := range requests {
		if err := request(ctx); err != nil {
			return err
		}
	}

	log.Println("harbor configuration initialized")
	return nil
}

func createProject(ctx context.Context) error {
	return executeHTTPRequest(ctx, "POST", "/projects", fmt.Sprintf(`{"project_name": "%s"}`, projectName))
}

func listProjects(ctx context.Context) error {
	return executeHTTPRequest(ctx, "GET", "/projects", "")
}
func listAdapters(ctx context.Context) error {
	return executeHTTPRequest(ctx, "GET", "/replication/adapters", "")
}

func listRegistries(ctx context.Context) error {
	return executeHTTPRequest(ctx, "GET", "/registries", "")
}

func pingRegistry(ctx context.Context) error {
	data := fmt.Sprintf(`{
		"access_key": "",
		"access_secret": "",
		"description": "",
		"insecure": true,
		"name": "%s",
		"type": "harbor-satellite",
		"url": "http://gc:8080/groups/sync"
	}`, registryName)

	return executeHTTPRequest(ctx, "POST", "/registries/ping", data)
}

func createRegistry(ctx context.Context) error {
	data := fmt.Sprintf(`{
		"credential": {
			"access_key": "",
			"access_secret": "",
			"type": "basic"
		},
		"description": "",
		"insecure": true,
		"name": "%s",
		"type": "harbor-satellite",
		"url": "http://gc:8080/groups/sync"
	}`, registryName)

	return executeHTTPRequest(ctx, "POST", "/registries", data)
}

func pushToRegistry(ctx context.Context) error {
	_, err := dag.Container().
		From("alpine:latest").
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithExec([]string{"apk", "add", "crane"}).
		WithExec([]string{"crane", "auth", "login", "core:8080", "-u", "admin", "-p", "Harbor12345", "--insecure"}).
		WithExec([]string{"cat", "/root/.docker/config.json"}).
		WithExec([]string{"crane", "copy", "docker.io/library/alpine:latest", "core:8080/edge/alpine:latest", "--insecure"}).
		Stdout(ctx)

	return err
}

func listArtifacts(ctx context.Context) error {
	return executeHTTPRequest(ctx, "GET", "/projects/edge/artifacts", "")
}

func createConfig(ctx context.Context) error {
	data := fmt.Sprintf(`{
		"config_name": "test-config",
		"registry": "%s",
		"config": {
			"app_config": {
				"ground_control_url": "http://gc:8080",
				"log_level": "info",
				"use_unsecure": true,
				"state_replication_interval": "@every 00h00m10s",
				"update_config_interval": "@every 00h00m10s",
				"register_satellite_interval": "@every 00h00m10s",
				"bring_own_registry": false,
				"local_registry": {
					"url": "http://127.0.0.1:8585"
				}
			},
			"zot_config": {
				"distSpecVersion": "1.1.0",
				"storage": {
					"rootDirectory": "./zot"
				},
				"http": {
					"address": "127.0.0.1",
					"port": "8585"
				},
				"log": {
					"level": "info"
				}
			}
		}
	}`, harborDomain)
	return executeHTTPRequest(ctx, "POST", "/configs", data)
}

func createReplicationPolicy(ctx context.Context) error {
	data := fmt.Sprintf(`{
		"name": "%s",
		"dest_registry": {
			"id": 1,
			"name": "%s",
			"status": "healthy",
			"type": "harbor-satellite",
			"url": "http://gc:8080/groups/sync"
		},
		"dest_namespace": "%s",
		"dest_namespace_replace_count": 1,
		"trigger": {
			"type": "manual",
			"trigger_settings": {
				"cron": ""
			}
		},
		"filters": [{
			"type": "name",
			"value": "edge/**"
		}],
		"enabled": true,
		"deletion": false,
		"override": true,
		"speed": -1
	}`, replicationPolicy, registryName, destNamespace)

	return executeHTTPRequest(ctx, "POST", "/replication/policies", data)
}

func executeReplication(ctx context.Context) error {
	data := fmt.Sprintf(`{ "policy_id": %d }`, policyId)
	return executeHTTPRequest(ctx, "POST", "/replication/executions", data)
}

func getExecuteReplication(ctx context.Context) error {
	url := fmt.Sprintf("/replication/executions/%d", 3)
	return executeHTTPRequest(ctx, "GET", url, "")
}

func executeHTTPRequest(ctx context.Context, method, endpoint, data string) error {
	args := []string{"curl", "-s", "-i", "-f", "-X", method}

	if endpoint == "/configs" {
		args = append(args, fmt.Sprintf("%s%s", "http://gc:8080", endpoint))
	} else {
		args = append(args, "-u", fmt.Sprintf("%s:%s", harborAdminUser, harborAdminPassword))
		args = append(args, fmt.Sprintf("%s%s", harborBaseURL, endpoint))
	}
	if data != "" {
		args = append(args, "-H", "Content-Type: application/json")
		args = append(args, "-d", data)
	}

	stdout, err := curlContainer(ctx, args)
	if err != nil {
		return fmt.Errorf("HTTP %s %s failed: %w", method, endpoint, err)
	}

	log.Printf("%s %s completed. response: %s", method, endpoint, stdout)
	return err
}

func curlContainer(ctx context.Context, cmd []string) (string, error) {
	return dag.Container().
		From("curlimages/curl@sha256:9a1ed35addb45476afa911696297f8e115993df459278ed036182dd2cd22b67b").
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithExec(cmd).
		Stdout(ctx)
}

func makeGroundControlRequest(ctx context.Context, method, path string, body any) (string, error) {
	var reqBody []byte
	var err error

	if body != nil {
		reqBody, err = json.Marshal(body)
		if err != nil {
			return "", fmt.Errorf("failed to marshal request body: %w", err)
		}
	}

	httpContainer := dag.Container().
		From("alpine@sha256:8a1f59ffb675680d47db6337b49d22281a139e9d709335b492be023728e11715").
		WithExec([]string{"apk", "add", "curl"}).
		WithEnvVariable("CACHEBUSTER", time.Now().String())

	var curlArgs []string
	curlArgs = append(curlArgs, "curl", "-sX", method)

	if body != nil {
		curlArgs = append(curlArgs, "-H", "Content-Type: application/json")
		curlArgs = append(curlArgs, "-d", string(reqBody))
	}

	curlArgs = append(curlArgs, fmt.Sprintf("http://gc:8080%s", path))

	stdout, err := httpContainer.WithExec(curlArgs).Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}

	log.Printf("ground control api %s %s response: %s", method, path, stdout)

	return stdout, nil
}

func checkHealthGroundControl(ctx context.Context) {
	cmd := []string{"curl", "-sif", "http://gc:8080/health"}

	_, err := curlContainer(ctx, cmd)
	if err != nil {
		log.Fatalf("health check failed for ground control service: %v", err)
	}
}

func (m *HarborSatellite) registerSatelliteAndZTR(ctx context.Context) {

	registerReq := map[string]any{
		"name":        "test-satellite",
		"groups":      []string{"test-group"},
		"config_name": "test-config",
	}

	registerResp, err := makeGroundControlRequest(ctx, "POST", "/satellites", registerReq)
	if err != nil {
		log.Fatalf("failed to register satellite: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(registerResp), &resp); err != nil {
		log.Fatalf("failed to unmarshal register satellite respone: %v", err)
	}

	token, exists := resp["token"]

	if !exists {
		log.Fatal("respone should contain token")
	}
	if token == "" {
		log.Fatal("token should not be empty")
	}

	log.Printf("satellite registered successfully with token: %v", token)

	//ZTR
	_, err = dag.Container().
		From("golang:1.24-alpine@sha256:68932fa6d4d4059845c8f40ad7e654e626f3ebd3706eef7846f319293ab5cb7a").
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod")).
		WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
		WithMountedCache("/go/build-cache", dag.CacheVolume("go-build")).
		WithEnvVariable("GOCACHE", "/go/build-cache").
		WithMountedDirectory("/app", m.Source.Directory(".")).
		WithWorkdir("/app").
		WithEnvVariable("TOKEN", token.(string)).
		WithEnvVariable("GROUND_CONTROL_URL", "http://gc:8080").
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithExec([]string{"go", "build", "-o", "satellite", "cmd/main.go"}).
		WithExec([]string{"cp", "config.example.json", "config.json"}).
		WithExposedPort(8585, dagger.ContainerWithExposedPortOpts{ExperimentalSkipHealthcheck: true}).
		WithEntrypoint([]string{"sh", "-c", "./satellite"}).
		AsService().WithHostname("satellite").Start(ctx)

	if err != nil {
		log.Fatalf("failed to start satellite: %v", err)
	}

	log.Println("Satellite startup and ZTR process completed successfully")
}

func (m *HarborSatellite) getImageFromZot(ctx context.Context) (string, error) {
	out, err := dag.Container().
		From("alpine:latest").
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithExec([]string{"apk", "add", "crane"}).
		WithExec([]string{"crane", "pull", "satellite:8585/edge/alpine:latest", "alpine.tar", "--insecure"}).
		WithExec([]string{"tar", "-xf", "alpine.tar"}).
		WithExec([]string{"cat", "manifest.json"}).
		Stdout(ctx)

	var e *dagger.ExecError
	if errors.As(err, &e) {
		return fmt.Sprintf("pipeline failure: %s", e.Stderr), nil
	} else if err != nil {
		return "", err
	}

	return out, nil
}
