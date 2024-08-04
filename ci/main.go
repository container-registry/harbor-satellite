package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"container-registry.com/harbor-satellite/ci/config"
	ground_control_ci "container-registry.com/harbor-satellite/ci/ground_control"
	satellite_ci "container-registry.com/harbor-satellite/ci/satellite"
	"dagger.io/dagger"
)

func main() {
	if len(os.Args) < 2 {
		slog.Error("Please provide the app name (satellite or ground-control) as an argument.")
		os.Exit(1)
	}

	appName := os.Args[1]

	token := os.Getenv("GITHUB_TOKEN")
	user := os.Getenv("GITHUB_USERNAME")
	repo := os.Getenv("GITHUB_REPOSITORY")
	sha := os.Getenv("GITHUB_SHA")

	if token == "" || user == "" || repo == "" || sha == "" {
		panic(fmt.Sprintf("Missing required environment variables: GITHUB_TOKEN=%s, GITHUB_USERNAME=%s, GITHUB_REPOSITORY=%s, GITHUB_SHA=%s", token, user, repo, sha))
	}

	config := config.Config{
		GithubToken:       token,
		GithubUser:        user,
		Github_Repository: repo,
		Github_SHA:        sha,
		AppName:           appName,
	}
	ctx := context.Background()

	// Initialize Dagger client
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		panic(err)
	}
	defer client.Close()

	switch appName {
	case "satellite":
		satelliteCI := satellite_ci.NewSatelliteCI(client, ctx, &config)
		err := satelliteCI.StartSatelliteCI()
		if err != nil {
			slog.Error("Error executing Satellite CI: " + err.Error())
			os.Exit(1)
		}

	case "ground-control":
		groundControlCI := ground_control_ci.NewGroundControlCI(client, ctx, &config)
		err := groundControlCI.StartGroundControlCI()
		if err != nil {
			slog.Error("Error executing Ground Control CI: " + err.Error())
			os.Exit(1)
		}

	default:
		slog.Error("Invalid app name. Please provide either 'satellite' or 'ground-control'.")
		os.Exit(1)
	}
}
