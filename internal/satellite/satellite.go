package satellite

import (
	"context"
	"fmt"
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
			for _, img := range imgs {
				err = s.replicator.Replicate(ctx, img.Reference)
				if err != nil {
					return err
				}
			}
		}
		fmt.Print("--------------------------------\n")
	}
}
