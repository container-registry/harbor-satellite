package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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
	"container-registry.com/harbor-satellite/logger"
	"container-registry.com/harbor-satellite/registry"
	"golang.org/x/sync/errgroup"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/spf13/viper"

	"github.com/joho/godotenv"
)

type ImageList struct {
	RegistryURL  string `json:"registryUrl"`
	Repositories []struct {
		Repository string `json:"repository"`
		Images     []struct {
			Name string `json:"name"`
		} `json:"images"`
	} `json:"repositories"`
}

func main() {
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		fmt.Println("Error reading config file, ", err)
		fmt.Println("Exiting Satellite")
		os.Exit(1)
	}

	err := run()
	if err != nil {
		os.Exit(1)
	}
}

func run() error {
	var fetcher store.ImageFetcher

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)

	logLevel := viper.GetString("log_level")
	ctx = logger.AddLoggerToContext(ctx, logLevel)

	log := logger.FromContext(ctx)
	log.Info().Msg("Satellite starting")

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
		ip := net.ParseIP(registryAdr)
		if ip == nil {
			log.Error().Msg("Invalid IP address")
			return errors.New("invalid IP address")
		}
		if ip.To4() != nil {
			log.Info().Msg("IP address is valid IPv4")
		} else {
			log.Error().Msg("IP address is IPv6 format and unsupported")
			return errors.New("IP address is IPv6 format and unsupported")
		}
		registryPort := viper.GetString("own_registry_port")
		os.Setenv("ZOT_URL", registryAdr+":"+registryPort)
	} else {
		log.Info().Msg("Launching default registry")
		g.Go(func() error {
			launch, err := registry.LaunchRegistry(viper.GetString("zotConfigPath"))
			if launch {
				cancel()
				return err
			}
			if err != nil {
				cancel()
				log.Error().Err(err).Msg("Failed to launch default registry")
				return err
			}
			return nil
		})
	}

	input := viper.GetString("url_or_file")
	parsedURL, err := url.Parse(input)
	if err != nil || parsedURL.Scheme == "" {
		if strings.ContainsAny(input, "\\:*?\"<>|") {
			log.Error().Msg("Path contains invalid characters. Please check the configuration.")
			return err
		}
		dir, err := os.Getwd()
		if err != nil {
			log.Error().Err(err).Msg("Error getting current directory")
			return err
		}
		absPath := filepath.Join(dir, input)
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			log.Error().Err(err).Msg("No URL or file found. Please check the configuration.")
			return err
		}
		log.Info().Msg("Input is a valid file path.")
		fetcher = store.FileImageListFetcher(ctx, input)
		os.Setenv("USER_INPUT", input)

		// Parse images.json and set environment variables
		file, err := os.Open(absPath)
		if err != nil {
			log.Error().Err(err).Msg("Error opening images.json file")
			return err
		}
		defer file.Close()

		var imageList ImageList
		if err := json.NewDecoder(file).Decode(&imageList); err != nil {
			log.Error().Err(err).Msg("Error decoding images.json file")
			return err
		}

		registryURL := imageList.RegistryURL
		registryParts := strings.Split(registryURL, "/")
		if len(registryParts) < 3 {
			log.Error().Msg("Invalid registryUrl format in images.json")
			return errors.New("invalid registryUrl format in images.json")
		}
		registry := registryParts[2]
		os.Setenv("REGISTRY", registry)

		if len(imageList.Repositories) > 0 {
			repository := imageList.Repositories[0].Repository
			os.Setenv("REPOSITORY", repository)
		} else {
			log.Error().Msg("No repositories found in images.json")
			return errors.New("no repositories found in images.json")
		}
	} else {
		log.Info().Msg("Input is a valid URL.")
		fetcher = store.RemoteImageListFetcher(ctx, input)
		os.Setenv("USER_INPUT", input)
		parts := strings.SplitN(input, "://", 2)
		scheme := parts[0] + "://"
		os.Setenv("SCHEME", scheme)
		registryAndPath := parts[1]
		registryParts := strings.Split(registryAndPath, "/")
		registry := registryParts[0]
		os.Setenv("REGISTRY", registry)
		apiVersion := registryParts[1]
		os.Setenv("API_VERSION", apiVersion)
		repository := registryParts[2]
		os.Setenv("REPOSITORY", repository)
		image := registryParts[3]
		os.Setenv("IMAGE", image)
	}

	err = godotenv.Load()
	if err != nil {
		log.Error().Err(err).Msg("Error loading.env file")
		return err
	}

	ctx, storer := store.NewInMemoryStore(ctx, fetcher)
	replicator := replicate.NewReplicator(ctx)
	s := satellite.NewSatellite(ctx, storer, replicator)

	g.Go(func() error {
		return s.Run(ctx)
	})
	log.Info().Msg("Satellite running")

	err = g.Wait()
	if err != nil {
		log.Error().Err(err).Msg("Error running satellite")
		return err
	}
	return nil
}
