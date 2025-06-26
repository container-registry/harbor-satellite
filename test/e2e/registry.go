package e2e

import (
	"context"
	"fmt"
	"log"
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

	configDirPath = "./test/e2e/testconfig/config/"

	postgresPort  = 5432
	redisPort     = 6379
	registryPort  = 5000
	corePort      = 8080
	coreDebugPort = 4001

	projectName       = "edge"
	registryName      = "test-registry"
	replicationPolicy = "satellite-group"
	destNamespace     = "group1"
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

	return hr.client.Container().
		From(coreImage).
		WithMountedFile("/etc/core/app.conf", coreConfig).
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

func (hr *HarborRegistry) SetupHarborRegistry() (*dagger.Service, error) {
	log.Println("setting up harbor registry environment...")

	if err := hr.startPostgres(); err != nil {
		return nil, fmt.Errorf("failed to start postgresql: %w", err)
	}
	log.Println("postgresql service started")

	if err := hr.startRedis(); err != nil {
		return nil, fmt.Errorf("failed to start redis: %w", err)
	}
	log.Println("redis service started")

	if err := hr.startRegistry(); err != nil {
		return nil, fmt.Errorf("failed to start registry: %w", err)
	}
	log.Println("registry service started")

	if err := hr.startRegistryCtl(); err != nil {
		return nil, fmt.Errorf("failed to start registryctl: %w", err)
	}
	log.Println("registryctl service started")

	coreService, err := hr.startCore()
	if err != nil {
		return nil, fmt.Errorf("failed to start Core service: %w", err)
	}
	log.Println("core service started")

	//TODO://Need to check the health and then proceed instead of arbitarysleep
	time.Sleep(1 * time.Minute)

	if err := hr.initializeHarborRegistry(); err != nil {
		return nil, fmt.Errorf("failed to initialize harbor registry: %w", err)
	}

	log.Println("harbor registry setup completed successfully")
	return coreService, nil
}

func (hr *HarborRegistry) initializeHarborRegistry() error {
	log.Println("initializing harbor registry...")

	requests := []func() error{
		hr.createProject,
		hr.listAdapters,
		hr.pingRegistry,
		hr.createRegistry,
		hr.listRegistries,
		hr.createReplicationPolicy,
	}

	for i, request := range requests {
		if err := request(); err != nil {
			return fmt.Errorf("failed to execute step %d: %w", i+1, err)
		}
	}

	log.Println("harbor configuration initialized")
	return nil
}

func (hr *HarborRegistry) createProject() error {
	return hr.executeHTTPRequest("POST", "/projects", fmt.Sprintf(`{"project_name": "%s"}`, projectName))
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
		"url": "http://gc:8080"
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
		"url": "http://gc:8080"
	}`, registryName)

	return hr.executeHTTPRequest("POST", "/registries", data)
}

// TODO:// Need to check if we below function can create replication policy
func (hr *HarborRegistry) createReplicationPolicy() error {
	data := fmt.Sprintf(`{
		"name": "%s",
		"dest_registry": {
			"creation_time": "2025-06-26T13:01:24.466Z",
			"credential": {},
			"id": 169,
			"name": "%s",
			"status": "healthy",
			"type": "harbor-satellite",
			"update_time": "2025-06-26T13:01:24.466Z",
			"url": "http://gc:8080"
		},
		"dest_namespace": "%s",
		"dest_namespace_replace_count": 1,
		"trigger": {
			"type": "manual",
			"trigger_settings": {
				"cron": ""
			}
		},
		"filters": [],
		"enabled": true,
		"deletion": false,
		"override": true,
		"speed": -1
	}`, replicationPolicy, registryName, destNamespace)

	return hr.executeHTTPRequest("POST", "/replication/policies", data)
}

//TODO:
// Execution
// https://core:8080/api/v2.0/replication/executions

func (hr *HarborRegistry) executeHTTPRequest(method, endpoint, data string) error {
	args := []string{"curl", "-s", "-X", method}

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

func curlContainer(ctx context.Context, c *dagger.Client, cmd []string) (string, error) {
	return c.Container().
		From("curlimages/curl@sha256:9a1ed35addb45476afa911696297f8e115993df459278ed036182dd2cd22b67b").
		WithExec(cmd).
		Stdout(ctx)
}
