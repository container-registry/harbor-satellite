package satellite

import (
	"context"
	"fmt"
	"time"

	"container-registry.com/harbor-satellite/internal/replicate"
	"container-registry.com/harbor-satellite/internal/store"
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
	// Execute the initial operation immediately without waiting for the ticker
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
		err = s.replicator.DeleteExtraImages(ctx, imgs)
		if err != nil {
			return err
		}
	}
	fmt.Print("--------------------------------\n")

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
				err = s.replicator.DeleteExtraImages(ctx, imgs)
				if err != nil {
					return err
				}
			}
		}
		fmt.Print("--------------------------------\n")
	}
}
