package main

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"dagger/harbor-satellite/internal/dagger"
)

// builds given component from source
func (m *HarborSatellite) build(source *dagger.Directory, component string) *dagger.Directory {
	fmt.Printf("Building %s\n", component)
	// Fetch supported builds (GOOS and GOARCH combinations)
	supportedBuilds := getSupportedBuilds()
	binaryName := component

	outputs := dag.Directory()

	// For satellite, the full source is mounted to /app and we work from there
	// For ground-control, only the ground-control subdir is mounted to /app
	workDir := PROJ_MOUNT

	golang := dag.Container().
		From(DEFAULT_GO).
		WithDirectory(PROJ_MOUNT, source).
		WithWorkdir(workDir).
		WithExec([]string{"ls", "-la"})

	// Iterate through supported builds
	for goos, goarches := range supportedBuilds {
		for _, goarch := range goarches {
			outputBinary := fmt.Sprintf("%s/%s-%s-%s", component, binaryName, goos, goarch)
			build := golang.
				WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod")).
				WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
				WithMountedCache("/go/build-cache", dag.CacheVolume("go-build")).
				WithEnvVariable("GOCACHE", "/go/build-cache").
				WithEnvVariable("GOOS", goos).
				WithEnvVariable("GOARCH", goarch)

			if component == "satellite" {
				build = build.WithExec([]string{"go", "build", "-o", outputBinary, "./cmd/main.go"})
			} else {
				build = build.WithExec([]string{"go", "build", "-o", outputBinary, "."})
			}
			outputs = outputs.WithDirectory(component, build.Directory(component))
		}
	}

	return outputs
}

// PrepareForRelease prepares the repository for a release by creating a new tag. The default release type is "patch".
func (m *HarborSatellite) get_release_tag(ctx context.Context, git_container *dagger.Container, source *dagger.Directory, name string,
	// +optional
	// +default="patch"
	release_type string,
) (string, error) {
	/// This would get the last tag that was created. Empty string if no tag was created.
	getTagsOutput, err := git_container.
		WithExec([]string{"git", "fetch", "--tags"}).
		WithExec([]string{
			"/bin/sh", "-c",
			fmt.Sprintf(`git tag --list "v*%s" | sort -V | tail -n 1`, name),
		}).
		Stdout(ctx)
	if err != nil {
		fmt.Println("Failed to get tags: ", err.Error(), ".")
		fmt.Println("Get Tags Output: ", getTagsOutput, ".")
		return getTagsOutput, err
	}
	fmt.Println("Get Tags Output:", getTagsOutput, ".")
	slog.Info("Get Tags Output:", getTagsOutput, ".")
	latest_tag := strings.TrimSpace(getTagsOutput)
	new_tag, err := generateNewTag(latest_tag, name, release_type)
	if err != nil {
		slog.Error("Failed to generate new tag: ", err.Error(), ".")
		return "", err
	}

	return new_tag, nil
}

func generateNewTag(latestTag, suffix, release_type string) (string, error) {
	if latestTag == "" {
		// If the latest tag is empty, this is the first release
		return fmt.Sprintf("v0.0.1-%s", suffix), nil
	}
	fmt.Println("Latest tag: ", latestTag)
	versionWithoutSuffix := strings.TrimSuffix(latestTag, fmt.Sprintf("-%s", suffix))
	versionWithoutSuffix = strings.TrimPrefix(versionWithoutSuffix, "v")
	fmt.Println("Version without suffix: ", versionWithoutSuffix)
	parts := strings.Split(versionWithoutSuffix, ".")
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		slog.Error("Failed to convert major version to integer: ", err.Error(), ".")
		return "", err
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		slog.Error("Failed to convert minor version to integer: ", err.Error(), ".")
		return "", err
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		slog.Error("Failed to convert patch version to integer: ", err.Error(), ".")
		return "", err
	}
	// Increment the version according to the release type
	switch release_type {
	case "major":
		major++
	case "minor":
		minor++
	case "patch":
		patch++
	}
	newVersion := fmt.Sprintf("v%d.%d.%d-%s", major, minor, patch, suffix)

	return newVersion, nil
}

func (m *HarborSatellite) getPathToReleaser(name string) (string, error) {
	switch name {
	case "satellite":
		return ".goreleaser.yaml", nil
	case "ground-control":
		return "ground-control/.goreleaser.yaml", nil
	default:
		return "", fmt.Errorf("unknown name: %s", name)
	}
}

func getSupportedBuilds() map[string][]string {
	return map[string][]string{
		"linux":  {"amd64", "arm64", "ppc64le", "s390x", "riscv64"},
		"darwin": {"amd64", "arm64"},
	}
}
