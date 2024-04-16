package satellite

import (
	"context"
	"time"

	"container-registry.com/harbor-satelite/internal/replicate"
	"container-registry.com/harbor-satelite/internal/store"
)

type Satellite struct {
	storer     store.Storer
	replicator replicate.Replicator
}

func (s *Satellite) Run(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Minute)
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
	}
}
