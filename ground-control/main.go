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
	serverResult := server.NewServer()
	httpServer := serverResult.Server
	tlsCfg := serverResult.TLSConfig
	spiffeCfg := serverResult.SPIFFEConfig

	go func() {
		var err error
		switch {
		case spiffeCfg != nil && spiffeCfg.Enabled:
			// SPIFFE provides certificates via TLSConfig.GetCertificate
			fmt.Printf("Starting Ground Control with SPIFFE mTLS on port %s\n", httpServer.Addr)
			err = httpServer.ListenAndServeTLS("", "")
		case tlsCfg.Enabled:
			fmt.Printf("Starting Ground Control with TLS on port %s\n", httpServer.Addr)
			err = httpServer.ListenAndServeTLS(tlsCfg.CertFile, tlsCfg.KeyFile)
		default:
			fmt.Printf("Starting Ground Control on port %s\n", httpServer.Addr)
			err = httpServer.ListenAndServe()
		}
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("cannot start server: %s", err)
		}
	}()

	fmt.Printf("Ground Control running on port %s\n", httpServer.Addr)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	shutdownCtx, shutdownRelease := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownRelease()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}
}
