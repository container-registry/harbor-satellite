package main

import (
	"context"
	"log/slog"
	"os"

	"dagger.io/dagger"
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
