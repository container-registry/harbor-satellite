package admission_test

import (
	"path/filepath"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/admission"
	"github.com/container-registry/harbor-satellite/internal/shared/policybundle"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestConfigSource_ReadsAdmission(t *testing.T) {
	pending.Plan(t, "7.5", "ConfigSource is a stub")
	dir := t.TempDir()
	b := bundle(allowAll, blockRepo)
	cm, err := config.NewConfigManager(filepath.Join(dir, "config.json"), filepath.Join(dir, "prev.json"), "", "http://gc.example", false, &config.Config{
		AppConfig: config.AppConfig{Admission: config.AdmissionConfig{Mode: "audit", Bundle: &b}},
	})
	require.NoError(t, err)
	src := admission.ConfigSource{CM: cm}

	got, mode, ok := src.Current()
	require.True(t, ok)
	require.Equal(t, policybundle.ModeAudit, mode)
	require.Equal(t, b, got)

	cm.With(func(c *config.Config) { c.AppConfig.Admission.Mode = "block" })
	_, mode, ok = src.Current()
	require.True(t, ok)
	require.Equal(t, policybundle.ModeEnforce, mode, "a mode that does not parse fails closed")

	cm.With(func(c *config.Config) { c.AppConfig.Admission = config.AdmissionConfig{} })
	_, _, ok = src.Current()
	require.False(t, ok, "no bundle")
}
