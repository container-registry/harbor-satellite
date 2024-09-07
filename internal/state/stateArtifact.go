package state

import (
	"context"
	"fmt"
	"os"

	"container-registry.com/harbor-satellite/internal/utils"
	"container-registry.com/harbor-satellite/logger"
	"github.com/google/go-containerregistry/pkg/crane"
)

// pulls State Artifact from given registry.
func PullStateArtifact(ctx context.Context, reg, groupName, tag string) error {
	log := logger.FromContext(ctx)
	// TO-DO: Implementation of reg groupName tag will be handled in ztr
	// imageRef := "demo.goharbor.io/satellite/satellite-01:latest"
	imageRef := fmt.Sprintf("%s/satellite/%s:%s", reg, groupName, tag)

	auth, err := utils.Auth()
	if err != nil {
		return fmt.Errorf("error in authentication: %v", err)
	}

	log.Info().Msg("Pulling State Artifact")

	// Pull the image with authentication
	img, err := crane.Pull(imageRef, crane.WithAuth(auth), crane.Insecure)
	if err != nil {
		// panic(fmt.Sprintf("Failed to pull image: %v", err))
		return fmt.Errorf("failed to pull state artifact: %v", err)
	}

	out, err := os.Create("state.json")
	if err != nil {
		return fmt.Errorf("failed to create state.json file: %v", err)
	}
	defer out.Close()

	// export image
	if err := crane.Export(img, out); err != nil {
		return fmt.Errorf("failed to write state.json file: %v", err)
	}

	log.Info().Msgf("successfully written state to: %s", out.Name())

	return nil
}
