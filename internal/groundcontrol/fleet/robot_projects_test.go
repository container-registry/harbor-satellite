package fleet_test

import (
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/stretchr/testify/require"
)

func TestRobotProjects(t *testing.T) {
	for name, tc := range map[string]struct {
		projects, want []string
	}{
		"no groups":           {nil, []string{fleet.SatelliteProject}},
		"one project":         {[]string{"library"}, []string{"library", fleet.SatelliteProject}},
		"duplicates":          {[]string{"team", "library", "team", "library"}, []string{"library", fleet.SatelliteProject, "team"}},
		"satellite included":  {[]string{fleet.SatelliteProject, "library"}, []string{"library", fleet.SatelliteProject}},
		"sorted, not ordered": {[]string{"zeta", "alpha"}, []string{"alpha", fleet.SatelliteProject, "zeta"}},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, fleet.RobotProjects(tc.projects))
		})
	}
}
