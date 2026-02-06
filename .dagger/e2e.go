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

	gcAdminUser     = "admin"
	gcAdminPassword = "AdminPass123"

	harborImageTag   = "satellite"
	postgresImage    = "goharbor/harbor-db:v2.14.0"
	redisImage       = "goharbor/redis-photon:v2.14.0"
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
	m.setupHarborRegistry(ctx)
	m.startPostgres(ctx)
	m.startGroundControl(ctx)
	m.initializeHarborRegistry(ctx)
	m.registerSatelliteAndZTR(ctx)
	return m.pullImageFromZot(ctx)
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
		From(DEFAULT_GO+"-alpine").
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
		WithEnvVariable("ADMIN_PASSWORD", gcAdminPassword).
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithDirectory("/migrations", gcDir.Directory("./sql/schema")).
		WithWorkdir("/app").
		WithExec([]string{"go", "build", "-o", "gc", "main.go"}).
		WithExposedPort(8080, dagger.ContainerWithExposedPortOpts{ExperimentalSkipHealthcheck: true}).
		WithEntrypoint([]string{"./gc"}).
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
	runScript := m.Source.File(configDirPath + "run_env.sh")
	privatekey := m.Source.File(configDirPath + "core/private_key.pem")

	return dag.Container().
		From(coreImage).
		WithMountedFile("/etc/core/app.conf", coreConfig).
		WithMountedFile("/etc/core/private_key.pem", privatekey).
		WithMountedFile("/envFile", envCoreFile).
		WithMountedFile("/run_script", runScript).
		WithExec([]string{"chmod", "+x", "/run_script"}).
		WithExposedPort(corePort, dagger.ContainerWithExposedPortOpts{ExperimentalSkipHealthcheck: true}).
		WithExposedPort(coreDebugPort, dagger.ContainerWithExposedPortOpts{ExperimentalSkipHealthcheck: true}).
		WithEntrypoint([]string{"/run_script", "/core"}).
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
func (m *HarborSatellite) setupHarborRegistry(ctx context.Context) {
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

	if err := m.waitForCoreServiceHealth(ctx); err != nil {
		requireNoExecError(err, "core service health check")
	}
	log.Println("core service health check passed")

	if err := m.startJobService(ctx); err != nil {
		requireNoExecError(err, "start job service")
	}
	log.Println("job service started")

	log.Println("harbor registry setup completed successfully")
}

func (m *HarborSatellite) waitForCoreServiceHealth(ctx context.Context) error {
	timeout := time.After(15 * time.Minute)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for services to be healthy")
		case <-ticker.C:
			_, err := m.executeHTTPRequest(ctx, "GET", "/health", "")
			if err == nil {
				log.Println("core service is healthy")
				return nil
			}
			log.Printf("Services not ready yet: %v", err)
		}
	}
}

func (m *HarborSatellite) initializeHarborRegistry(ctx context.Context) {
	log.Println("initializing harbor registry...")

	requests := []func(ctx context.Context) (string, error){
		m.createProject,
		m.listProjects,
		m.pushToRegistry,
		m.listArtifacts,
		m.listAdapters,
		m.pingRegistry,
		m.createRegistry,
		m.listRegistries,
		m.createReplicationPolicy,
		m.executeReplication,
		m.getExecuteReplication,
		m.createConfig,
		m.createGroup,
	}

	for _, request := range requests {
		if _, err := request(ctx); err != nil {
			requireNoExecError(err, "initialize harbor registry")
		}
	}

	log.Println("harbor configuration initialized")
}

func (m *HarborSatellite) createProject(ctx context.Context) (string, error) {
	return m.executeHTTPRequest(ctx, "POST", "/projects", fmt.Sprintf(`{"project_name": "%s"}`, projectName))
}

func (m *HarborSatellite) listProjects(ctx context.Context) (string, error) {
	return m.executeHTTPRequest(ctx, "GET", "/projects", "")
}
func (m *HarborSatellite) listAdapters(ctx context.Context) (string, error) {
	return m.executeHTTPRequest(ctx, "GET", "/replication/adapters", "")
}

func (m *HarborSatellite) listRegistries(ctx context.Context) (string, error) {
	return m.executeHTTPRequest(ctx, "GET", "/registries", "")
}

func (m *HarborSatellite) pingRegistry(ctx context.Context) (string, error) {
	data := fmt.Sprintf(`{
		"access_key": "",
		"access_secret": "",
		"description": "",
		"insecure": true,
		"name": "%s",
		"type": "harbor-satellite",
		"url": "http://gc:8080/groups/sync"
	}`, registryName)

	return m.executeHTTPRequest(ctx, "POST", "/registries/ping", data)
}

func (m *HarborSatellite) createRegistry(ctx context.Context) (string, error) {
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

	return m.executeHTTPRequest(ctx, "POST", "/registries", data)
}

func (m *HarborSatellite) pushToRegistry(ctx context.Context) (string, error) {
	_, err := dag.Container().
		From("alpine:latest").
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithExec([]string{"apk", "add", "crane"}).
		WithExec([]string{"crane", "auth", "login", "core:8080", "-u", "admin", "-p", "Harbor12345", "--insecure"}).
		WithExec([]string{"cat", "/root/.docker/config.json"}).
		WithExec([]string{"crane", "copy", "gcr.io/distroless/static:latest", "core:8080/edge/alpine:latest", "--insecure"}).
		Stdout(ctx)

	return "", err
}

func (m *HarborSatellite) listArtifacts(ctx context.Context) (string, error) {
	return m.executeHTTPRequest(ctx, "GET", "/projects/edge/artifacts", "")
}

func (m *HarborSatellite) createConfig(ctx context.Context) (string, error) {
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
	return m.executeHTTPRequest(ctx, "POST", "/api/configs", data)
}

func (m *HarborSatellite) createGroup(ctx context.Context) (string, error) {
	data := fmt.Sprintf(`{
		"group": "%s",
		"registry": "%s",
		"artifacts": [{"repository": "%s/alpine", "tag": ["latest"]}]
	}`, destNamespace, harborDomain, projectName)
	return m.executeHTTPRequest(ctx, "POST", "/api/groups/sync", data)
}

func (m *HarborSatellite) createReplicationPolicy(ctx context.Context) (string, error) {
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

	return m.executeHTTPRequest(ctx, "POST", "/replication/policies", data)
}

func (m *HarborSatellite) executeReplication(ctx context.Context) (string, error) {
	data := fmt.Sprintf(`{ "policy_id": %d }`, policyId)
	return m.executeHTTPRequest(ctx, "POST", "/replication/executions", data)
}

func (m *HarborSatellite) getExecuteReplication(ctx context.Context) (string, error) {
	url := fmt.Sprintf("/replication/executions/%d", 3)
	return m.executeHTTPRequest(ctx, "GET", url, "")
}

func (m *HarborSatellite) getGCAuthToken(ctx context.Context) (string, error) {
	if m.gcAuthToken != "" {
		return m.gcAuthToken, nil
	}

	loginData := fmt.Sprintf(`{"username":"%s","password":"%s"}`, gcAdminUser, gcAdminPassword)
	args := []string{"curl", "-sf", "-X", "POST", "http://gc:8080/login", "-H", "Content-Type: application/json", "-d", loginData}

	stdout, err := curlContainer(ctx, args)
	if err != nil {
		return "", fmt.Errorf("login failed: %w", err)
	}

	// Parse token from response: {"token":"...","expires_at":"..."}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		return "", fmt.Errorf("failed to parse login response: %w (response: %s)", err, stdout)
	}

	if resp.Token == "" {
		return "", fmt.Errorf("received empty token from login response")
	}

	m.gcAuthToken = resp.Token
	log.Printf("Got GC auth token")
	return m.gcAuthToken, nil
}

func (m *HarborSatellite) executeHTTPRequest(ctx context.Context, method, endpoint, data string) (string, error) {
	args := []string{"curl", "-sf", "-X", method}

	gcEndpoints := map[string]bool{
		"/api/configs":             true,
		"/api/satellites":          true,
		"/api/satellites/register": true,
		"/api/groups/sync":         true,
		"/api/spire/agents":        true,
	}

	if gcEndpoints[endpoint] {
		token, err := m.getGCAuthToken(ctx)
		if err != nil {
			return "", err
		}
		args = append(args, "-H", fmt.Sprintf("Authorization: Bearer %s", token))
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
		return "", fmt.Errorf("HTTP %s %s failed: %w", method, endpoint, err)
	}

	log.Printf("%s %s completed. response: %s", method, endpoint, stdout)
	return stdout, err
}

func curlContainer(ctx context.Context, cmd []string) (string, error) {
	return dag.Container().
		From("curlimages/curl@sha256:9a1ed35addb45476afa911696297f8e115993df459278ed036182dd2cd22b67b").
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithExec(cmd).
		Stdout(ctx)
}

func checkHealthGroundControl(ctx context.Context) {
	timeout := time.After(2 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	cmd := []string{"curl", "-sif", "http://gc:8080/health"}

	for {
		select {
		case <-timeout:
			log.Fatalf("timeout waiting for ground control health check")
		case <-ticker.C:
			_, err := curlContainer(ctx, cmd)
			if err == nil {
				log.Println("ground control service is healthy")
				return
			}
			log.Printf("ground control not ready yet: %v", err)
		}
	}
}

func (m *HarborSatellite) registerSatelliteAndZTR(ctx context.Context) {

	registerReq := fmt.Sprintf(`{
		"name": "test-satellite",
		"groups": ["%s"],
		"config_name": "test-config"
	}`, destNamespace)

	registerResp, err := m.executeHTTPRequest(ctx, "POST", "/api/satellites", registerReq)
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
		From(DEFAULT_GO+"-alpine").
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

func (m *HarborSatellite) pullImageFromZot(ctx context.Context) (string, error) {
	out, err := dag.Container().
		From("alpine:latest").
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithExec([]string{"apk", "add", "crane"}).
		WithExec([]string{"sleep", "25s"}).
		WithExec([]string{"crane", "pull", "satellite:8585/edge/alpine:latest", "alpine.tar", "--insecure"}).
		WithExec([]string{"tar", "-xf", "alpine.tar"}).
		WithExec([]string{"cat", "manifest.json"}).
		Stdout(ctx)

	if err != nil {
		return "", fmt.Errorf("error unable to pull image from zot registry: %w", err)
	}

	return out, nil
}

// TestSpiffeJoinTokenE2E tests the SPIFFE join token flow with embedded SPIRE server.
// This test verifies:
// 1. GC starts with embedded SPIRE server
// 2. Satellites can be registered via /api/satellites/register with attestation_method=join_token
// 3. Satellite with SPIRE agent can attest using the join token
func (m *HarborSatellite) TestSpiffeJoinTokenE2E(ctx context.Context) (string, error) {
	log.Println("Starting SPIFFE Join Token E2E test...")

	// Start PostgreSQL
	m.startPostgres(ctx)
	log.Println("PostgreSQL started")

	// Start Ground Control with embedded SPIRE
	m.startGroundControlWithEmbeddedSPIRE(ctx)
	log.Println("Ground Control with embedded SPIRE started")

	// Register satellite and get join token
	log.Println("Registering satellite with join token attestation...")
	joinTokenResp, err := m.registerSatelliteWithJoinToken(ctx, "test-satellite", "us-west")
	if err != nil {
		return "", fmt.Errorf("join token generation failed: %w", err)
	}
	log.Printf("Join token response: %s", joinTokenResp)

	// Parse response to extract join token
	var tokenResp map[string]any
	if err := json.Unmarshal([]byte(joinTokenResp), &tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse join token response: %w", err)
	}

	joinToken, exists := tokenResp["join_token"].(string)
	if !exists || joinToken == "" {
		return "", fmt.Errorf("join token response missing 'join_token' field")
	}
	if _, exists := tokenResp["spiffe_id"]; !exists {
		return "", fmt.Errorf("join token response missing 'spiffe_id' field")
	}

	// Start satellite with SPIRE agent using the join token
	log.Println("Starting satellite with SPIRE agent...")
	err = m.startSatelliteWithSPIRE(ctx, joinToken)
	if err != nil {
		return "", fmt.Errorf("failed to start satellite with SPIRE: %w", err)
	}
	log.Println("Satellite with SPIRE agent started and attested successfully")

	log.Println("SPIFFE Join Token E2E test completed successfully")
	return joinTokenResp, nil
}

func (m *HarborSatellite) startGroundControlWithEmbeddedSPIRE(ctx context.Context) {
	gcDir := m.Source.Directory("./ground-control")

	_, err := dag.Container().
		From(DEFAULT_GO+"-alpine").
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod")).
		WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
		WithMountedCache("/go/build-cache", dag.CacheVolume("go-build")).
		WithEnvVariable("GOCACHE", "/go/build-cache").
		WithDirectory("/app", gcDir).
		WithWorkdir("/app").
		// Database config
		WithEnvVariable("DB_HOST", "postgres").
		WithEnvVariable("DB_PORT", "5432").
		WithEnvVariable("DB_USERNAME", "postgres").
		WithEnvVariable("DB_PASSWORD", "password").
		WithEnvVariable("DB_DATABASE", "groundcontrol").
		WithEnvVariable("PORT", "8080").
		// Embedded SPIRE config - bind to 0.0.0.0 for external access
		WithEnvVariable("EMBEDDED_SPIRE_ENABLED", "true").
		WithEnvVariable("SPIRE_DATA_DIR", "/tmp/spire-data").
		WithEnvVariable("SPIRE_TRUST_DOMAIN", "harbor-satellite.local").
		WithEnvVariable("SPIRE_BIND_ADDRESS", "0.0.0.0").
		// Skip Harbor health check for this test
		WithEnvVariable("SKIP_HARBOR_HEALTH_CHECK", "true").
		WithEnvVariable("ADMIN_PASSWORD", gcAdminPassword).
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithDirectory("/migrations", gcDir.Directory("./sql/schema")).
		// Install SPIRE server binary
		WithExec([]string{"apk", "add", "--no-cache", "curl", "tar"}).
		WithExec([]string{"sh", "-c",
			"curl -sL https://github.com/spiffe/spire/releases/download/v1.10.4/spire-1.10.4-linux-amd64-musl.tar.gz | tar xz -C /opt"}).
		WithEnvVariable("PATH", "/opt/spire-1.10.4/bin:/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin").
		WithExec([]string{"go", "build", "-o", "gc", "main.go"}).
		WithExposedPort(8080, dagger.ContainerWithExposedPortOpts{ExperimentalSkipHealthcheck: true}).
		WithExposedPort(8081, dagger.ContainerWithExposedPortOpts{ExperimentalSkipHealthcheck: true}). // SPIRE server port
		WithEntrypoint([]string{"./gc"}).
		AsService().WithHostname("gc").Start(ctx)

	if err != nil {
		log.Fatalf("failed to start ground control with embedded SPIRE: %v", err)
	}

	// Wait for GC to be healthy
	m.waitForGCHealthWithRetry(ctx, 60*time.Second)

	// Wait for SPIRE server port to be accessible
	m.waitForSPIREServer(ctx, 30*time.Second)
}

// startSatelliteWithSPIRE starts a satellite with SPIRE agent that attests using the join token.
func (m *HarborSatellite) startSatelliteWithSPIRE(ctx context.Context, joinToken string) error {
	// Create SPIRE agent config - join_token attestor config is empty,
	// the actual token is passed via -joinToken flag
	agentConfig := `agent {
    data_dir = "/tmp/spire-agent"
    log_level = "DEBUG"
    server_address = "gc"
    server_port = 8081
    socket_path = "/tmp/spire-agent/agent.sock"
    trust_domain = "harbor-satellite.local"
    insecure_bootstrap = true
}

plugins {
    NodeAttestor "join_token" {
        plugin_data {}
    }

    KeyManager "memory" {
        plugin_data {}
    }

    WorkloadAttestor "unix" {
        plugin_data {}
    }
}
`

	// Start container with SPIRE agent and verify attestation
	out, err := dag.Container().
		From(DEFAULT_GO+"-alpine").
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		// Install SPIRE agent binary and netcat for debugging
		WithExec([]string{"apk", "add", "--no-cache", "curl", "tar", "netcat-openbsd"}).
		WithExec([]string{"sh", "-c",
			"curl -sL https://github.com/spiffe/spire/releases/download/v1.10.4/spire-1.10.4-linux-amd64-musl.tar.gz | tar xz -C /opt"}).
		WithEnvVariable("PATH", "/opt/spire-1.10.4/bin:/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin").
		// Create agent config
		WithExec([]string{"mkdir", "-p", "/tmp/spire-agent"}).
		WithNewFile("/tmp/spire-agent/agent.conf", agentConfig).
		// Pass join token as environment variable for use in script
		WithEnvVariable("JOIN_TOKEN", joinToken).
		// Debug: check connectivity to SPIRE server
		WithExec([]string{"sh", "-c", "echo 'Testing connectivity to gc:8081...' && nc -zv gc 8081 || echo 'Connection failed'"}).
		// Start SPIRE agent with join token and wait for attestation
		WithExec([]string{"sh", "-c", `
			echo "Starting SPIRE agent with join token..."
			# Start SPIRE agent with join token flag
			spire-agent run -config /tmp/spire-agent/agent.conf -joinToken "$JOIN_TOKEN" 2>&1 &
			AGENT_PID=$!

			# Give agent time to start and attest
			sleep 5

			# Wait for agent to attest (check socket exists)
			for i in $(seq 1 30); do
				if [ -S /tmp/spire-agent/agent.sock ]; then
					echo "SPIRE agent socket ready"
					# Verify we can fetch SVID
					if spire-agent api fetch -socketPath /tmp/spire-agent/agent.sock; then
						echo "SVID fetch successful - attestation complete"
						kill $AGENT_PID 2>/dev/null || true
						exit 0
					fi
				fi
				echo "Waiting for SPIRE agent... ($i/30)"
				sleep 2
			done

			echo "SPIRE agent attestation failed"
			kill $AGENT_PID 2>/dev/null || true
			exit 1
		`}).
		Stdout(ctx)

	if err != nil {
		return fmt.Errorf("SPIRE agent attestation failed: %w", err)
	}

	log.Printf("SPIRE agent output: %s", out)
	return nil
}

func (m *HarborSatellite) waitForGCHealthWithRetry(ctx context.Context, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if time.Now().After(deadline) {
				log.Fatalf("timeout waiting for Ground Control to be healthy")
			}

			cmd := []string{"curl", "-sf", "http://gc:8080/health"}
			_, err := curlContainer(ctx, cmd)
			if err == nil {
				log.Println("Ground Control is healthy")
				return
			}
			log.Printf("Ground Control not ready yet, retrying...")
		}
	}
}

func (m *HarborSatellite) waitForSPIREServer(ctx context.Context, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if time.Now().After(deadline) {
				log.Fatalf("timeout waiting for SPIRE server to be ready")
			}

			// Check if SPIRE server port is accessible
			_, err := dag.Container().
				From("alpine:latest").
				WithExec([]string{"apk", "add", "--no-cache", "netcat-openbsd"}).
				WithExec([]string{"nc", "-zv", "gc", "8081"}).
				Stdout(ctx)
			if err == nil {
				log.Println("SPIRE server port is accessible")
				return
			}
			log.Printf("SPIRE server not ready yet, retrying...")
		}
	}
}

func (m *HarborSatellite) registerSatelliteWithJoinToken(ctx context.Context, satelliteName, region string) (string, error) {
	data := fmt.Sprintf(`{
		"satellite_name": "%s",
		"region": "%s",
		"selectors": ["unix:uid:0"],
		"attestation_method": "join_token",
		"ttl_seconds": 600
	}`, satelliteName, region)

	return m.executeHTTPRequest(ctx, "POST", "/api/satellites/register", data)
}
