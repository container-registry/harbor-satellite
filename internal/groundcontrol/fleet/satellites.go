package fleet

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

type SatelliteService struct{}

type RegisterSatelliteCmd struct {
	Name, ConfigName string
	Groups           []string
}

type RegistrationToken struct {
	Token     string
	ExpiresAt time.Time
}

// Register creates a token satellite in its groups with its config, a
// robot that may pull the union of the groups' projects and the satellite
// project, and its state artifact, groupless included (D10). Any failure
// rolls the repository back and deletes a robot it created. A satellite or
// robot that already exists is ErrAlreadyExists.
func (s *SatelliteService) Register(ctx context.Context, cmd RegisterSatelliteCmd) (RegistrationToken, error) {
	return RegistrationToken{}, notimpl.Err("1.2 register satellite")
}

func (s *SatelliteService) Delete(ctx context.Context, name string) error {
	return notimpl.Err("1.2 delete satellite")
}

func (s *SatelliteService) Get(ctx context.Context, name string) (Satellite, error) {
	return Satellite{}, notimpl.Err("1.2 get satellite")
}

func (s *SatelliteService) List(ctx context.Context) ([]Satellite, error) {
	return nil, notimpl.Err("1.2 list satellites")
}
