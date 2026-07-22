package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	if err := cli.RootCmd().ExecuteContext(ctx); err != nil {
		stop()
		os.Exit(1)
	}
	stop()
}
