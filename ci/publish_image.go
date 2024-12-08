package main

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	// +defaultPath="./"
	source *dagger.Directory,
) []string {
	var directory *dagger.Directory
	switch {
	case component == "satellite":
		directory = source
	case component == "ground-control":
		directory = source.Directory(GROUND_CONTROL_PATH)
	default:
		panic(fmt.Sprintf("unknown component: %s", component))
	}
	dirContainer := dag.Container().From("alpine:latest").
		WithMountedDirectory(PROJ_MOUNT, directory).
		WithExec([]string{"ls", PROJ_MOUNT})
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
	for i := range builders {
		builder := builders[i]
		os, err := builder.EnvVariable(ctx, "GOOS")
		if err != nil {
			panic(fmt.Sprintf("failed to get GOOS: %v", err))
		}
		arch, err := builder.EnvVariable(ctx, "GOARCH")
		if err != nil {
			panic(fmt.Sprintf("failed to get GOARCH: %v", err))
		}
		if os != "linux" {
			continue
		}
		fmt.Printf("Building image for %s/%s\n", os, arch)

		ctr := dag.Container(dagger.ContainerOpts{Platform: dagger.Platform(os + "/" + arch)}).
			From("alpine:latest").
			WithFile(component, builder.File(component)).
			WithEntrypoint([]string{"./" + component})
		releaseImages = append(releaseImages, ctr)
	}
	imageAddrs := []string{}
	password, err := registryPassword.Plaintext(ctx)
	if err != nil {
		panic(fmt.Sprintf("failed to get password: %v", err))
	}
	password = strings.TrimSpace(password)
	for _, imageTag := range imageTags {
		addr, err := dag.Container().WithRegistryAuth(registry, registryUsername, dag.SetSecret("password", password)).
			Publish(ctx,
				fmt.Sprintf("%s/%s/%s:%s", registry, "harbor-satellite", component, imageTag),
				dagger.ContainerPublishOpts{PlatformVariants: releaseImages},
			)
		if err != nil {
			panic(fmt.Sprintf("failed to publish image: %v", err))
		}
		fmt.Println("Published image:", addr)
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
	// +optional
	// +default=["latest"]
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
	// +optional
	// +default=true
	useRegistryPassword bool,
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
			useRegistryPassword,
		)
		if err != nil {
			return "", fmt.Errorf("failed to sign image: %w", err)
		}
		fmt.Printf("Signed image: %s\n", imageAddrs[i])
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
	// +optional
	// +default=true
	useRegistryPassword bool,
) (string, error) {
	// set the context deadline to 5 minutes to avoid the error of "context deadline exceeded" when signing the image
	// setting it to 5 minutes because the signing process may take a while also the token is valid for 5 minutes
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	registryPasswordPlain, err := registryPassword.Plaintext(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get password: %w", err)
	}
	registryPasswordPlain = strings.TrimSpace(registryPasswordPlain)
	cosing_ctr := dag.Container().From("cgr.dev/chainguard/cosign")

	if !useRegistryPassword {
		// If githubToken is provided, use it to sign the image
		if githubToken != nil {
			if actionsIdTokenRequestUrl == "" || actionsIdTokenRequestToken == nil {
				return "", fmt.Errorf("actionsIdTokenRequestUrl (exist=%s) and actionsIdTokenRequestToken (exist=%t) must be provided when githubToken is provided", actionsIdTokenRequestUrl, actionsIdTokenRequestToken != nil)
			}
			githubTokenPlain, err := githubToken.Plaintext(ctx)
			if err != nil {
				return "", fmt.Errorf("failed to get github token: %w", err)
			}
			githubTokenPlain = strings.TrimSpace(githubTokenPlain)

			actionsIdTokenRequestTokenPlain, err := actionsIdTokenRequestToken.Plaintext(ctx)
			if err != nil {
				return "", fmt.Errorf("failed to get actionsIdTokenRequestToken: %w", err)
			}
			actionsIdTokenRequestTokenPlain = strings.TrimSpace(actionsIdTokenRequestTokenPlain)
			fmt.Printf("Setting the ENV Vars GITHUB_TOKEN, ACTIONS_ID_TOKEN_REQUEST_URL, ACTIONS_ID_TOKEN_REQUEST_TOKEN to sign with GitHub Token")
			cosing_ctr = cosing_ctr.WithEnvVariable("GITHUB_TOKEN", githubTokenPlain).
				WithEnvVariable("ACTIONS_ID_TOKEN_REQUEST_URL", actionsIdTokenRequestUrl).
				WithEnvVariable("ACTIONS_ID_TOKEN_REQUEST_TOKEN", actionsIdTokenRequestTokenPlain)
		}
	}
	return cosing_ctr.WithEnvVariable("REGISTRY_PASSWORD", registryPasswordPlain).
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
				WithMountedDirectory(PROJ_MOUNT, source).
				WithWorkdir(PROJ_MOUNT).
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
