package harbor

import (
	"testing"

	"github.com/container-registry/harbor-satellite/internal/env"
	"github.com/stretchr/testify/require"
)

func loadGCForTest(t *testing.T) {
	t.Helper()

	previous := env.GC
	require.NoError(t, env.LoadGC())
	t.Cleanup(func() {
		env.GC = previous
	})
}

func TestRobotDurationDays(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want int64
	}{
		{
			name: "default when unset",
			env:  "",
			want: 30,
		},
		{
			name: "custom value",
			env:  "90",
			want: 90,
		},
		{
			name: "invalid string falls back to default",
			env:  "abc",
			want: 30,
		},
		{
			name: "zero falls back to default",
			env:  "0",
			want: 30,
		},
		{
			name: "negative falls back to default",
			env:  "-5",
			want: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv("ROBOT_DURATION_DAYS", tt.env)
			} else {
				t.Setenv("ROBOT_DURATION_DAYS", "")
			}
			loadGCForTest(t)
			got := robotDurationDays()
			require.Equal(t, tt.want, got)
		})
	}
}

func TestRobotAccountTemplate_Duration(t *testing.T) {
	t.Setenv("ROBOT_DURATION_DAYS", "60")
	loadGCForTest(t)

	tmpl := RobotAccountTemplate("test-sat", []string{"satellite"})

	require.Equal(t, int64(60), tmpl.Duration)
	require.Equal(t, "test-sat", tmpl.Name)
	require.Equal(t, "system", tmpl.Level)
	require.False(t, tmpl.Disable)
}
