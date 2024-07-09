package satellite

import (
	"context"
	"time"

	"container-registry.com/harbor-satellite/internal/replicate"
	"container-registry.com/harbor-satellite/internal/store"
	"container-registry.com/harbor-satellite/logger"
)

type Satellite struct {
	storer     store.Storer
	replicator replicate.Replicator
}

func NewSatellite(ctx context.Context, storer store.Storer, replicator replicate.Replicator) *Satellite {
	return &Satellite{
		storer:     storer,
		replicator: replicator,
	}
}

func (s *Satellite) Run(ctx context.Context) error {
	log := logger.FromContext(ctx)

	// Execute the initial operation immediately without waiting for the ticker
	imgs, err := s.storer.List(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Error listing images")
		return err
	}
	if len(imgs) == 0 {
		log.Info().Msg("No images to replicate")
	} else {
		for _, img := range imgs {
			err = s.replicator.Replicate(ctx, img.Name)
			if err != nil {
				log.Error().Err(err).Msg("Error replicating image")
				return err
			}
		}
		s.replicator.DeleteExtraImages(ctx, imgs)
	}
	log.Info().Msg("--------------------------------\n")

	// Temporarily set to faster tick rate for testing purposes
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			imgs, err := s.storer.List(ctx)
			if err != nil {
				log.Error().Err(err).Msg("Error listing images")
				return err
			}
			if len(imgs) == 0 {
				log.Info().Msg("No images to replicate")
			} else {
				for _, img := range imgs {
					err = s.replicator.Replicate(ctx, img.Name)
					if err != nil {
						log.Error().Err(err).Msg("Error replicating image")
						return err
					}
				}
				s.replicator.DeleteExtraImages(ctx, imgs)
			}
		}
		log.Info().Msg("--------------------------------\n")
	}
}
