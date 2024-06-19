package main

import (
	"context"
	"log/slog"
	"os"

	"dagger.io/dagger"
)

const (
	imageVersion  = "golang:1.22" // Use a constant for the Go image version
	exposePort    = 9090          // Client port to expose
	containerPort = 9090          // Container port to expose
	appDir        = "/app"        // Directory inside the container
	appBinary     = "app"         // Name of the built application
	sourceFile    = "main.go"     // Source file to build
)

func main() {
	ctx := context.Background()

	// initialize Dagger client
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// use a golang:1.19 container
	// get version
	// execute
	golang := client.Container().From("golang:1.21").WithExec([]string{"go", "version"})

	version, err := golang.Stdout(ctx)
	if err != nil {
		panic(err)
	}
	// print output
	slog.Info("Hello from Dagger!", "version", version)
}
