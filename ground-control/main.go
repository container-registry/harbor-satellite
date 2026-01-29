package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/container-registry/harbor-satellite/ground-control/internal/harborhealth"
	"github.com/container-registry/harbor-satellite/ground-control/internal/server"
	"github.com/container-registry/harbor-satellite/ground-control/migrator"
	_ "github.com/joho/godotenv/autoload"
)

func main() {
	err := harborhealth.CheckHealth()
	if err != nil {
		log.Fatalf("health check failed: %v", err)
	}

	migrator.DoMigrations()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := server.NewServer(ctx)

	go func() {
		var err error
		if server.TLSEnabled {
			err = srv.ListenAndServeTLS(server.TLSCertPath, server.TLSKeyPath)
		} else {
			err = srv.ListenAndServe()
		}
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("cannot start server: %s", err)
		}
	}()

	fmt.Printf("Ground Control running on port %s\n", srv.Addr)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	cancel()

	shutdownCtx, shutdownRelease := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownRelease()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("HTTP shutdown error: %v", err)
	}
}
