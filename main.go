package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"container-registry.com/harbor-satelite/internal/replicate"
	"container-registry.com/harbor-satelite/internal/satellite"
	"container-registry.com/harbor-satelite/internal/store"
	"golang.org/x/sync/errgroup"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	err := run()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{}))
	mux.Handle("/debug/pprof/", http.HandlerFunc(pprof.Index))
	mux.Handle("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))
	mux.Handle("/debug/pprof/trace", http.HandlerFunc(pprof.Trace))
	mux.Handle("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol))
	mux.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	mux.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	mux.Handle("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))
	mux.Handle("/debug/pprof/block", pprof.Handler("block"))
	mux.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))
	metricsSrv := &http.Server{
		Addr:    ":9090",
		Handler: mux,
	}
	g.Go(func() error {
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	g.Go(func() error {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return metricsSrv.Shutdown(shutdownCtx)
	})

	// Prompt the user to choose between remote and file fetcher
	fmt.Println("Choose an image list fetcher:")
	fmt.Println("1. Remote")
	fmt.Println("2. File")
	fmt.Print("Enter your choice (1 or 2): ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read user input: %w", err)
	}

	var fetcher store.ImageFetcher
	switch input {
	case "1\n":
		fetcher = store.RemoteImageListFetcher()
	case "2\n":
		fetcher = store.FileImageListFetcher()
	default:
		return fmt.Errorf("invalid choice")
	}

	// Instantiate a new Satellite and its components
	storer := store.NewInMemoryStore(fetcher)
	replicator := replicate.NewReplicator()
	s := satellite.NewSatellite(storer, replicator)

	// Run the Satellite
	if err := s.Run(ctx); err != nil {
		fmt.Println("Error running satellite:", err)
		os.Exit(1)
	}

	g.Go(func() error {
		return s.Run(ctx)
	})

	err = g.Wait()
	if err != nil {
		return err
	}
	return nil
}
