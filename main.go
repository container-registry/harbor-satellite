package main

import (
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

	"container-registry.com/harbor-satellite/internal/replicate"
	"container-registry.com/harbor-satellite/internal/satellite"
	"container-registry.com/harbor-satellite/internal/store"
	"container-registry.com/harbor-satellite/registry"
	"golang.org/x/sync/errgroup"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/viper"

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
	var fetcher store.ImageFetcher

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)

	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(".")

	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("fatal error config file: %w", err)
	}

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

	bringOwnRegistry := viper.GetBool("bring_own_registry")
	if bringOwnRegistry {
		registryAdr := viper.GetString("own_registry_adr")

		// Validate registryAdr format
		// matched, err := regexp.MatchString(`^127\.0\.0\.1:\d{1,5}$`, registryAdr)
		// if err != nil {
		// 	return fmt.Errorf("error validating registry address: %w", err)
		// }
		// if matched {
		// 	return fmt.Errorf("invalid registry address format: %s", registryAdr)
		// }
		os.Setenv("ZOT_URL", registryAdr)
		fmt.Println("Registry URL set to:", registryAdr)
	} else {
		g.Go(func() error {
			launch, err := registry.LaunchRegistry()
			if launch {
				cancel()
				return nil
			} else {
				log.Println("Error launching registry :", err)
				cancel()
				return err
			}
		})
	}

	input := viper.GetString("url_or_file")
	parsedURL, err := url.Parse(input)
	if err != nil || parsedURL.Scheme == "" {
		if strings.ContainsAny(input, "\\:*?\"<>|") {
			fmt.Println("Path contains invalid characters. Please check the configuration.")
			return fmt.Errorf("invalid file path")
		}
		dir, err := os.Getwd()
		if err != nil {
			fmt.Println("Error getting current directory:", err)
			return err
		}
		absPath := filepath.Join(dir, input)
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			fmt.Println("No URL or file found. Please check the configuration.")
			return fmt.Errorf("file not found")
		}
		fmt.Println("Input is a valid file path.")
		fetcher = store.FileImageListFetcher(input)
		os.Setenv("USER_INPUT", input)
	} else {
		fmt.Println("Input is a valid URL.")
		fetcher = store.RemoteImageListFetcher(input)
		os.Setenv("USER_INPUT", input)
		parts := strings.SplitN(input, "://", 2)
		scheme := parts[0] + "://"
		os.Setenv("SCHEME", scheme)
		hostAndPath := parts[1]
		hostParts := strings.Split(hostAndPath, "/")
		host := hostParts[0]
		os.Setenv("HOST", host)
		apiVersion := hostParts[1]
		os.Setenv("API_VERSION", apiVersion)
		registry := hostParts[2]
		os.Setenv("REGISTRY", registry)
		repository := hostParts[3]
		os.Setenv("REPOSITORY", repository)
	}

	err = godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading.env file: %v", err)
	}

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
