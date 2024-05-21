package satellite

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"container-registry.com/harbor-satelite/internal/replicate"
	"container-registry.com/harbor-satelite/internal/store"
)

type Satellite struct {
	storer     store.Storer
	replicator replicate.Replicator
}

func NewSatellite(storer store.Storer, replicator replicate.Replicator) *Satellite {
	return &Satellite{
		storer:     storer,
		replicator: replicator,
	}
}

func (s *Satellite) Run(ctx context.Context) error {
	s.StartZotRegistry()
	// Temporarily set to faster tick rate for testing purposes
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			imgs, err := s.storer.List(ctx)
			if err != nil {
				return err
			}
			if len(imgs) == 0 {
				fmt.Println("No images to replicate")
			} else {
				for _, img := range imgs {
					err = s.replicator.Replicate(ctx, img.Name)
					if err != nil {
						return err
					}
				}
			}

		}
		fmt.Print("--------------------------------\n")
	}
}

func (s *Satellite) StartZotRegistry() error {
	fmt.Println("Starting Zot registry")
	dir, err := os.Getwd()
	if err != nil {
		fmt.Println("Error getting current directory:", err)
		return nil
	}

	registryDir := dir + "/registry"
	zotExecutablePath := registryDir + "/zot-darwin-arm64"
	configurationPath := registryDir + "/config.json"

	cmd := exec.Command(zotExecutablePath, "serve", configurationPath)
	cmd.Dir = registryDir

	// Capture output and error
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Start()
	if err != nil {
		fmt.Println("Zot registry failed to start:", err)
		fmt.Println("Command output:", stdout.String())
		fmt.Println("Command error:", stderr.String())
		return fmt.Errorf("failed to start zot registry: %w", err)
	}
	fmt.Println("Zot registry started successfully")
	return nil
}
