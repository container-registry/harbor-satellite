package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/pprof"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"container-registry.com/harbor-satelite/internal/replicate"
	"container-registry.com/harbor-satelite/internal/satellite"
	"container-registry.com/harbor-satelite/internal/store"
	"golang.org/x/sync/errgroup"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/joho/godotenv"
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

	var fetcher store.ImageFetcher
	for {
		fmt.Print("Enter the source (Repository URL or relative file path): ")

		// For testing purposes :
		// https://demo.goharbor.io/v2/myproject/album-server
		// Local file path : /image-list/images.json

		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error reading input. Please try again.")
			continue
		}
		input = strings.TrimSpace(input)

		// Try to parse the input as a URL
		parsedURL, err := url.Parse(input)
		if err != nil || parsedURL.Scheme == "" {
			// If there was an error, the input is not a valid URL.
			fmt.Println("Input is not a valid URL. Checking if it is a file path...")
			// Check if the input is a file path
			if strings.ContainsAny(input, "\\:*?\"<>|") {
				fmt.Println("Path contains invalid characters. Please try again.")
				continue
			}
			// Get the current working directory
			dir, err := os.Getwd()
			if err != nil {
				fmt.Println("Error getting current directory:", err)
				continue
			}

			// Construct the absolute path from the relative path
			absPath := filepath.Join(dir, input)

			// Check if the file exists
			if _, err := os.Stat(absPath); os.IsNotExist(err) {
				fmt.Println("No URL or file found. Please try again.")
				continue
			}
			fmt.Println("Input is a valid file path.")
			fetcher = store.FileImageListFetcher(input)
			os.Setenv("USER_INPUT", input)
		} else {
			fmt.Println("Input is a valid URL.")
			// If there was no error, the input is a valid URL.
			fetcher = store.RemoteImageListFetcher(input)
			os.Setenv("USER_INPUT", input)
			// Split the URL into scheme and the rest
			parts := strings.SplitN(input, "://", 2)
			scheme := parts[0] + "://"
			os.Setenv("SCHEME", scheme)
			hostAndPath := parts[1]

			// Split the host and path
			hostParts := strings.Split(hostAndPath, "/")

			// Set useful environment variables
			host := hostParts[0]
			os.Setenv("HOST", host)
			apiVersion := hostParts[1]
			os.Setenv("API_VERSION", apiVersion)
			registry := hostParts[2]
			os.Setenv("REGISTRY", registry)
			repository := hostParts[3]
			os.Setenv("REPOSITORY", repository)

		}
		break
	}
	// Load.env file
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading.env file: %v", err)
	}

	// Instantiate a new Satellite and its components
	storer := store.NewInMemoryStore(fetcher)
	replicator := replicate.NewReplicator()
	s := satellite.NewSatellite(storer, replicator)

	g.Go(func() error {
		return s.Run(ctx)
	})

	err = g.Wait()
	if err != nil {
		return err
	}
	return nil
}
