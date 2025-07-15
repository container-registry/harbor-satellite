package e2e

import (
	"context"
	"fmt"
	"log"
	"testing"
	"time"

	"dagger.io/dagger"
)

const (
	harborBaseURL       = "http://core:8080/api/v2.0"
	harborAdminUser     = "admin"
	harborAdminPassword = "Harbor12345"

	harborImageTag   = "test-satellite"
	postgresImage    = "narharim/postgres-harbor:" + harborImageTag
	redisImage       = "narharim/redis-harbor:" + harborImageTag
	registryImage    = "narharim/registry-harbor:" + harborImageTag
	registryCtlImage = "narharim/registryctl-harbor:" + harborImageTag
	coreImage        = "narharim/core-harbor:" + harborImageTag
	jobImage         = "narharim/job-harbor:" + harborImageTag

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

type HarborRegistry struct {
	ctx        context.Context
	client     *dagger.Client
	projectDir string
}

func NewHarborRegistry(ctx context.Context, client *dagger.Client, projectDir string) (*HarborRegistry, error) {
	return &HarborRegistry{
		ctx:        ctx,
		client:     client,
		projectDir: projectDir,
	}, nil
}

func (hr *HarborRegistry) startPostgres() error {
	_, err := hr.client.Container().
		From(postgresImage).
		WithExposedPort(postgresPort).
		AsService().
		WithHostname("postgresql").
		Start(hr.ctx)

	return err
}

func (hr *HarborRegistry) startRedis() error {
	_, err := hr.client.Container().
		From(redisImage).
		WithExposedPort(redisPort).
		AsService().
		WithHostname("redis").
		Start(hr.ctx)

	return err
}

func (hr *HarborRegistry) startRegistry() error {
	source := hr.client.Host().Directory(hr.projectDir)
	regConfigDir := source.Directory(configDirPath + "registry")

	_, err := hr.client.Container().
		From(registryImage).
		WithMountedDirectory("/etc/registry", regConfigDir).
		WithExposedPort(registryPort).
		WithoutExposedPort(5001).
		WithoutExposedPort(5443).
		AsService().
		WithHostname("registry").
		Start(hr.ctx)

	return err
}

func (hr *HarborRegistry) startRegistryCtl() error {
	source := hr.client.Host().Directory(hr.projectDir)

	regConfigDir := source.Directory(configDirPath + "registry")
	regCtlConfig := source.File(configDirPath + "registryctl/config.yml")
	envFile := source.File(configDirPath + "jobservice/env")
	runScript := source.File(configDirPath + "run_env.sh")

	_, err := hr.client.Container().
		From(registryCtlImage).
		WithMountedDirectory("/etc/registry", regConfigDir).
		WithMountedFile("/etc/registryctl/config.yml", regCtlConfig).
		WithMountedFile("/envFile", envFile).
		WithMountedFile("/run_script", runScript).
		WithExec([]string{"chmod", "+x", "/run_script"}).
		WithEntrypoint([]string{"/run_script", "/registryctl -c /etc/registryctl/config.yml"}).
		AsService().
		WithHostname("registryctl").
		Start(hr.ctx)

	return err
}

func (hr *HarborRegistry) startCore() (*dagger.Service, error) {
	source := hr.client.Host().Directory(hr.projectDir)

	coreConfig := source.File(configDirPath + "core/app.conf")
	envCoreFile := source.File(configDirPath + "core/env")
	runDebug := source.File(configDirPath + "run_debug.sh")
	privatekey := source.File(configDirPath + "core/private_key.pem")

	return hr.client.Container().
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
		Start(hr.ctx)

}

func (hr *HarborRegistry) startJobService() error {
	source := hr.client.Host().Directory(hr.projectDir)

	jobSrvConfig := source.File(configDirPath + "jobservice/config.yml")
	envFile := source.File(configDirPath + "jobservice/env")
	run_script := source.File(configDirPath + "run_env.sh")

	_, err := hr.client.Container().
		From(jobImage).
		WithMountedFile("/etc/jobservice/config.yml", jobSrvConfig).
		WithMountedDirectory("/var/log/jobs", source.Directory(configDirPath+"jobservice")).
		WithMountedFile("/envFile", envFile).
		WithMountedFile("/run_script", run_script).
		WithExec([]string{"chmod", "+x", "/run_script"}).
		WithExposedPort(8080).
		WithEntrypoint([]string{"/run_script", "/jobservice -c /etc/jobservice/config.yml"}).
		AsService().
		WithHostname("jobservice").
		Start(hr.ctx)

	return err
}

func (hr *HarborRegistry) SetupHarborRegistry(t *testing.T) {
	t.Log("setting up harbor registry environment...")

	if err := hr.startPostgres(); err != nil {
		requireNoExecError(t, err, "start postgresql")
	}
	t.Log("postgresql service started")

	if err := hr.startRedis(); err != nil {
		requireNoExecError(t, err, "start redis")
	}
	t.Log("redis service started")

	if err := hr.startRegistry(); err != nil {
		requireNoExecError(t, err, "start registry")
	}
	t.Log("registry service started")

	if err := hr.startRegistryCtl(); err != nil {
		requireNoExecError(t, err, "start registryctl")
	}
	t.Log("registryctl service started")

	_, err := hr.startCore()
	if err != nil {
		requireNoExecError(t, err, "start core service")
	}
	t.Log("core service started")

	if err := hr.waitForCoreServiceHealth(t); err != nil {
		requireNoExecError(t, err, "core service health check")
	}
	t.Log("core service health check passed")

	if err := hr.startJobService(); err != nil {
		requireNoExecError(t, err, "start job service")
	}
	t.Log("job service started")

	if err := hr.initializeHarborRegistry(t); err != nil {
		requireNoExecError(t, err, "initialize harbor registry")
	}

	t.Log("harbor registry setup completed successfully")
}

func (hr *HarborRegistry) waitForCoreServiceHealth(t *testing.T) error {
	timeout := time.After(15  * time.Minute)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for services to be healthy")
		case <-ticker.C:
			err := hr.executeHTTPRequest("GET", "/health", "")
			if err == nil {
				t.Log("core service is healthy")
				return nil
			}
			t.Logf("Services not ready yet: %v", err)
		}
	}
}

func (hr *HarborRegistry) initializeHarborRegistry(t *testing.T) error {
	t.Log("initializing harbor registry...")

	requests := []func() error{
		hr.createProject,
		hr.listProjects,
		hr.pushToRegistry,
		hr.listArtifacts,
		hr.listAdapters,
		hr.pingRegistry,
		hr.createRegistry,
		hr.listRegistries,
		hr.createReplicationPolicy,
		hr.executeReplication,
		hr.getExecuteReplication,
	}

	for _, request := range requests {
		if err := request(); err != nil {
			return err
		}
	}

	time.Sleep(2 * time.Minute)

	hr.getExecuteReplication()
	t.Log("harbor configuration initialized")
	return nil
}

func (hr *HarborRegistry) createProject() error {
	return hr.executeHTTPRequest("POST", "/projects", fmt.Sprintf(`{"project_name": "%s"}`, projectName))
}

func (hr *HarborRegistry) listProjects() error {
	return hr.executeHTTPRequest("GET", "/projects", "")
}
func (hr *HarborRegistry) listAdapters() error {
	return hr.executeHTTPRequest("GET", "/replication/adapters", "")
}

func (hr *HarborRegistry) listRegistries() error {
	return hr.executeHTTPRequest("GET", "/registries", "")
}

func (hr *HarborRegistry) pingRegistry() error {
	data := fmt.Sprintf(`{
		"access_key": "",
		"access_secret": "",
		"description": "",
		"insecure": true,
		"name": "%s",
		"type": "harbor-satellite",
		"url": "https://webhook-test.com/8cd208a7fbfc0918f4f3e11c78d2ac60"
	}`, registryName)

	return hr.executeHTTPRequest("POST", "/registries/ping", data)
}

func (hr *HarborRegistry) createRegistry() error {
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
		"url": "https://webhook-test.com/8cd208a7fbfc0918f4f3e11c78d2ac60"
	}`, registryName)

	return hr.executeHTTPRequest("POST", "/registries", data)
}

func (hr *HarborRegistry) pushToRegistry() error {
	_, err := hr.client.Container().
		From("alpine:latest").
		WithEnvVariable("CACHEBUSTER", time.Now().String()).
		WithExec([]string{"apk", "add", "crane"}).
		WithExec([]string{"crane", "auth", "login", "core:8080", "-u", "admin", "-p", "Harbor12345", "--insecure"}).
		WithExec([]string{"cat", "/root/.docker/config.json"}).
		WithExec([]string{"crane", "copy", "docker.io/library/alpine:latest", "core:8080/edge/alpine:latest", "--insecure"}).
		Stdout(hr.ctx)

	return err
}

func (hr *HarborRegistry) listArtifacts() error {
	return hr.executeHTTPRequest("GET", "/projects/edge/artifacts", "")
}

func (hr *HarborRegistry) createReplicationPolicy() error {
	data := fmt.Sprintf(`{
		"name": "%s",
		"dest_registry": {
			"id": 1,
			"name": "%s",
			"status": "healthy",
			"type": "harbor-satellite",
			"url": "https://webhook-test.com/8cd208a7fbfc0918f4f3e11c78d2ac60"
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

	return hr.executeHTTPRequest("POST", "/replication/policies", data)
}

func (hr *HarborRegistry) executeReplication() error {
	data := fmt.Sprintf(`{ "policy_id": %d }`, policyId)
	return hr.executeHTTPRequest("POST", "/replication/executions", data)
}

func (hr *HarborRegistry) getExecuteReplication() error {
	url := fmt.Sprintf("/replication/executions/%d", 3)
	return hr.executeHTTPRequest("GET", url, "")
}

func (hr *HarborRegistry) executeHTTPRequest(method, endpoint, data string) error {
	args := []string{"curl", "-s", "-i", "-f", "-X", method}

	args = append(args, "-u", fmt.Sprintf("%s:%s", harborAdminUser, harborAdminPassword))

	args = append(args, fmt.Sprintf("%s%s", harborBaseURL, endpoint))

	if data != "" {
		args = append(args, "-H", "Content-Type: application/json")
		args = append(args, "-d", data)
	}

	stdout, err := curlContainer(hr.ctx, hr.client, args)
	if err != nil {
		return fmt.Errorf("HTTP %s %s failed: %w", method, endpoint, err)
	}

	log.Printf("%s %s completed. response: %s", method, endpoint, stdout)
	return err
}
