package main

import (
	"context"
	"fmt"
	"strings"

	"container-registry.com/harbor-satellite/ci/internal/dagger"
)

// PublishImage publishes a container image to a registry with a specific tag and signs it using Cosign.
func (m *HarborSatellite) PublishImage(
	ctx context.Context,
	registry, registryUsername string,
	// +optional
	// +default=["latest"]
	imageTags []string,
	registryPassword *dagger.Secret,
	component string,
	// +optional
	// +defaultPath="."
	source *dagger.Directory,
) []string {
	var directory *dagger.Directory
	switch {
	case component == "satellite":
		directory = source
	case component == "ground-control":
		directory = source.Directory("ground-control")
	default:
		panic(fmt.Sprintf("unknown component: %s", component))
	}
	dirContainer := dag.Container().From("alpine:latest").WithMountedDirectory("/src", directory).WithExec([]string{"ls", "/src"})
	dirContainer.Stdout(ctx)
	builders := m.getBuildContainer(ctx, component, directory)
	releaseImages := []*dagger.Container{}

	for i, tag := range imageTags {
		imageTags[i] = strings.TrimSpace(tag)
		if strings.HasPrefix(imageTags[i], "v") {
			imageTags[i] = strings.TrimPrefix(imageTags[i], "v")
		}
	}
	fmt.Printf("provided tags: %s\n", imageTags)
	fmt.Printf("Total images to build: %d\n", len(builders))
	for _, builder := range builders {
		os, _ := builder.EnvVariable(ctx, "GOOS")
		arch, _ := builder.EnvVariable(ctx, "GOARCH")
		fmt.Printf("Building image for %s/%s\n", os, arch)
		if os != "linux" || "os" != "darwin" {
			continue
		}

		ctr := dag.Container(dagger.ContainerOpts{Platform: dagger.Platform(os + "/" + arch)}).
			From("alpine:latest").
			WithFile(component, builder.File(component)).
			WithEntrypoint([]string{"./" + component})
		releaseImages = append(releaseImages, ctr)
	}
	password, err := registryPassword.Plaintext(ctx)
	if err != nil {
		panic(fmt.Sprintf("failed to get plaintext: %v", err))
	}
	fmt.Printf("Password: %s", password)
	fmt.Printf("Registry: %s", registry)
	fmt.Printf("Username: %s", registryUsername)
	imageAddrs := []string{}
	for _, imageTag := range imageTags {
		addr, err := dag.Container().WithRegistryAuth(registry, registryUsername, registryPassword).
			Publish(ctx,
				fmt.Sprintf("%s/%s/%s:%s", registry, component, component, imageTag),
				dagger.ContainerPublishOpts{PlatformVariants: releaseImages},
			)

		if err != nil {
			panic(fmt.Sprintf("failed to publish image: %v", err))
		}
		fmt.Printf("Published image address: %s\n", addr)
		imageAddrs = append(imageAddrs, addr)
	}
	return imageAddrs
}

// PublishImageAndSign builds and publishes container images to a registry with a specific tags and then signs them using Cosign.
func (m *HarborSatellite) PublishImageAndSign(
	ctx context.Context,
	registry string,
	registryUsername string,
	registryPassword *dagger.Secret,
	imageTags []string,
	// +optional
	githubToken *dagger.Secret,
	// +optional
	actionsIdTokenRequestToken *dagger.Secret,
	// +optional
	actionsIdTokenRequestUrl string,
	component string,
	// +optional
	// +defaultPath="."
	source *dagger.Directory,
) (string, error) {

	imageAddrs := m.PublishImage(ctx, registry, registryUsername, imageTags, registryPassword, component, source)

	for i := range imageAddrs {
		_, err := m.Sign(
			ctx,
			githubToken,
			actionsIdTokenRequestUrl,
			actionsIdTokenRequestToken,
			registryUsername,
			registryPassword,
			imageAddrs[i],
		)
		if err != nil {
			return "", fmt.Errorf("failed to sign image: %w", err)
		}
		fmt.Printf("Signed image: %s\n", imageAddrs)
	}

	return imageAddrs[0], nil
}

// Sign signs a container image using Cosign, works also with GitHub Actions
func (m *HarborSatellite) Sign(ctx context.Context,
	// +optional
	githubToken *dagger.Secret,
	// +optional
	actionsIdTokenRequestUrl string,
	// +optional
	actionsIdTokenRequestToken *dagger.Secret,
	registryUsername string,
	registryPassword *dagger.Secret,
	imageAddr string,
) (string, error) {
	registryPasswordPlain, _ := registryPassword.Plaintext(ctx)

	cosing_ctr := dag.Container().From("cgr.dev/chainguard/cosign")

	// If githubToken is provided, use it to sign the image
	if githubToken != nil {
		if actionsIdTokenRequestUrl == "" || actionsIdTokenRequestToken == nil {
			return "", fmt.Errorf("actionsIdTokenRequestUrl (exist=%s) and actionsIdTokenRequestToken (exist=%t) must be provided when githubToken is provided", actionsIdTokenRequestUrl, actionsIdTokenRequestToken != nil)
		}
		fmt.Printf("Setting the ENV Vars GITHUB_TOKEN, ACTIONS_ID_TOKEN_REQUEST_URL, ACTIONS_ID_TOKEN_REQUEST_TOKEN to sign with GitHub Token")
		cosing_ctr = cosing_ctr.WithSecretVariable("GITHUB_TOKEN", githubToken).
			WithEnvVariable("ACTIONS_ID_TOKEN_REQUEST_URL", actionsIdTokenRequestUrl).
			WithSecretVariable("ACTIONS_ID_TOKEN_REQUEST_TOKEN", actionsIdTokenRequestToken)
	}

	return cosing_ctr.WithSecretVariable("REGISTRY_PASSWORD", registryPassword).
		WithExec([]string{"cosign", "env"}).
		WithExec([]string{"cosign", "sign", "--yes", "--recursive",
			"--registry-username", registryUsername,
			"--registry-password", registryPasswordPlain,
			imageAddr,
			"--timeout", "1m",
		}).Stdout(ctx)
}

func (m *HarborSatellite) getBuildContainer(
	ctx context.Context,
	component string,
	source *dagger.Directory,
) []*dagger.Container {
	var builds []*dagger.Container

	fmt.Println("🛠️  Building with Dagger...")
	supportedBuilds := getSupportedBuilds()
	for goos, arches := range supportedBuilds {
		for _, goarch := range arches {
			bin_path := fmt.Sprintf("build/%s/%s/", goos, goarch)
			builder := dag.Container().
				From(DEFAULT_GO+"-alpine").
				WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod-"+GO_VERSION)).
				WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
				WithMountedCache("/go/build-cache", dag.CacheVolume("go-build-"+GO_VERSION)).
				WithEnvVariable("GOCACHE", "/go/build-cache").
				WithMountedDirectory("/src", source).
				WithWorkdir("/src").
				WithEnvVariable("GOOS", goos).
				WithEnvVariable("GOARCH", goarch).
				WithExec([]string{"go", "build", "-o", bin_path + component, "."}).
				WithWorkdir(bin_path).
				WithExec([]string{"ls"}).
				WithEntrypoint([]string{"./" + component})

			_, err := builder.Stderr(ctx)
			if err != nil {
				panic(fmt.Sprintf("failed to get stderr: %v", err))
			}
			builds = append(builds, builder)
		}
	}
	return builds
}

func getSupportedBuilds() map[string][]string {
	return map[string][]string{
		"linux":  {"amd64", "arm64", "ppc64le", "s390x", "386", "loong64", "mips64", "mips64le", "mips", "mipsle", "riscv64"},
		"darwin": {"amd64", "arm64"},
	}
}
