package satellite

import (
	"context"
	"testing"

	runtime "github.com/container-registry/harbor-satellite/internal/container_runtime"
	"github.com/container-registry/harbor-satellite/pkg/config"
)

func TestNewSatellite(t *testing.T) {
	var dummyCM *config.ConfigManager // nil is fine for simple initialization checks
	dummyCRI := []runtime.CRIConfigResult{}

	// FIX: Use t.TempDir() instead of hardcoded "/tmp/dummy.json"
	dummyPath := t.TempDir() + "/dummy.json"
	headless := true

	s := NewSatellite(dummyCM, dummyCRI, dummyPath, headless)

	if s == nil {
		t.Fatal("Expected a Satellite instance, got nil")
	}
	if s.headless != headless {
		t.Errorf("Expected headless to be %v, got %v", headless, s.headless)
	}
	if s.stateFilePath != dummyPath {
		t.Errorf("Expected state file path %q, got %q", dummyPath, s.stateFilePath)
	}
	if len(s.schedulers) != 0 {
		t.Errorf("Expected 0 schedulers on initialization, got %d", len(s.schedulers))
	}
}

func TestSatellite_Run_Headless(t *testing.T) {
	s := NewSatellite(nil, nil, "", true)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel it immediately

	err := s.Run(ctx)

	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}
	if len(s.GetSchedulers()) != 0 {
		t.Errorf("Expected 0 schedulers to be created in headless mode, got %d", len(s.GetSchedulers()))
	}
}

func TestSatellite_Run_Headless_ActiveContext(t *testing.T) {
	s := NewSatellite(nil, nil, "", true)

	ctx := context.Background()
	err := s.Run(ctx)

	if err != nil {
		t.Errorf("Expected nil error with active context in headless mode, got: %v", err)
	}

	if len(s.GetSchedulers()) != 0 {
		t.Errorf("Expected 0 schedulers in headless mode, got %d", len(s.GetSchedulers()))
	}
}

func TestSatellite_Stop_EmptySchedulers(t *testing.T) {
	s := NewSatellite(nil, nil, "", true)
	ctx := context.Background()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Stop panicked when given empty schedulers: %v", r)
		}
	}()

	s.Stop(ctx)
}
