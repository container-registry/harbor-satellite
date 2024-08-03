package main

import (
	"context"
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
	}

	appName := os.Args[1]

	token := os.Getenv("GITHUB_TOKEN")
	user := os.Getenv("GITHUB_USERNAME")
	repo := os.Getenv("GITHUB_REPOSITORY")
	sha := os.Getenv("GITHUB_SHA")

	if token == "" || user == "" || repo == "" || sha == "" {
		panic("Missing required environment variables")
	}

	config := config.Config{
		GithubToken:       token,
		GithubUser:        user,
		Github_Repository: repo,
		Github_SHA:        sha,
		AppName:           appName,
	}
	ctx := context.Background()

	// initialize Dagger client
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		panic(err)
	}
	defer client.Close()

	switch appName {
	case "satellite":
		satelliteCI := satellite_ci.NewSatelliteCI(client, &ctx, &config)
		err := satelliteCI.StartSatelliteCI()
		if err != nil {
			slog.Error("Error executing Satellite CI: " + err.Error())
			panic(err)
		}

	case "ground-control":
		ground_control_ci := ground_control_ci.NewGroundControlCI(client, &ctx, &config)
		err := ground_control_ci.StartGroundControlCI()
		if err != nil {
			slog.Error("Error executing Ground Control CI: " + err.Error())
			panic(err)
		}
	default:
		slog.Error("Invalid app name. Please provide either 'satellite' or 'ground-control'.")
	}
}
