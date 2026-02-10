package runtime

import (
	"testing"
)

func TestParseMirrorFlags(t *testing.T) {
	tests := []struct {
		name    string
		mirrors []string
		want    []CRIConfig
		wantErr bool
	}{
		{
			name:    "single CRI with single registry",
			mirrors: []string{"containerd:docker.io"},
			want: []CRIConfig{
				{CRI: CRIContainerd, Registries: []string{"docker.io"}},
			},
		},
		{
			name:    "single CRI with multiple registries",
			mirrors: []string{"containerd:docker.io,quay.io"},
			want: []CRIConfig{
				{CRI: CRIContainerd, Registries: []string{"docker.io", "quay.io"}},
			},
		},
		{
			name:    "multiple CRIs",
			mirrors: []string{"containerd:docker.io", "docker:true"},
			want: []CRIConfig{
				{CRI: CRIContainerd, Registries: []string{"docker.io"}},
				{CRI: CRIDocker, Registries: []string{"true"}},
			},
		},
		{
			name:    "invalid format missing colon",
			mirrors: []string{"containerd-docker.io"},
			wantErr: true,
		},
		{
			name:    "empty input",
			mirrors: []string{},
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMirrorFlags(tt.mirrors)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseMirrorFlags() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			if len(got) != len(tt.want) {
				t.Fatalf("got %d configs, want %d", len(got), len(tt.want))
			}
			for i := range tt.want {
				if got[i].CRI != tt.want[i].CRI {
					t.Errorf("config[%d].CRI = %q, want %q", i, got[i].CRI, tt.want[i].CRI)
				}
				if len(got[i].Registries) != len(tt.want[i].Registries) {
					t.Errorf("config[%d].Registries = %v, want %v", i, got[i].Registries, tt.want[i].Registries)
				}
			}
		})
	}
}

func TestResolveCRIConfigs(t *testing.T) {
	t.Run("explicit mirrors take priority", func(t *testing.T) {
		mirrors := []string{"containerd:docker.io"}
		configs, err := ResolveCRIConfigs(mirrors, true, []string{"quay.io"}, []string{"docker"})
		if err != nil {
			t.Fatal(err)
		}
		// Should use mirrors, not autoDetect runtimes/registries
		if len(configs) != 1 {
			t.Fatalf("expected 1 config, got %d", len(configs))
		}
		if configs[0].CRI != CRIContainerd {
			t.Errorf("expected containerd, got %s", configs[0].CRI)
		}
	})

	t.Run("autoDetect false returns nil", func(t *testing.T) {
		configs, err := ResolveCRIConfigs(nil, false, []string{"docker.io"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if configs != nil {
			t.Fatalf("expected nil, got %v", configs)
		}
	})

	t.Run("autoDetect with explicit runtimes uses them", func(t *testing.T) {
		configs, err := ResolveCRIConfigs(nil, true, []string{"docker.io", "quay.io"}, []string{"containerd", "crio"})
		if err != nil {
			t.Fatal(err)
		}
		if len(configs) != 2 {
			t.Fatalf("expected 2 configs, got %d", len(configs))
		}
		if configs[0].CRI != CRIContainerd {
			t.Errorf("expected containerd, got %s", configs[0].CRI)
		}
		if configs[1].CRI != CRICrio {
			t.Errorf("expected crio, got %s", configs[1].CRI)
		}
		// registries should be passed through
		if len(configs[0].Registries) != 2 {
			t.Errorf("expected 2 registries for containerd, got %d", len(configs[0].Registries))
		}
	})

	t.Run("docker CRI gets true registries", func(t *testing.T) {
		configs, err := ResolveCRIConfigs(nil, true, []string{"docker.io"}, []string{"docker"})
		if err != nil {
			t.Fatal(err)
		}
		if len(configs) != 1 {
			t.Fatalf("expected 1 config, got %d", len(configs))
		}
		if configs[0].Registries[0] != "true" {
			t.Errorf("expected docker registries to be [true], got %v", configs[0].Registries)
		}
	})

	t.Run("mixed runtimes: docker gets true, others get registries", func(t *testing.T) {
		configs, err := ResolveCRIConfigs(nil, true, []string{"docker.io", "quay.io"}, []string{"docker", "containerd"})
		if err != nil {
			t.Fatal(err)
		}
		if len(configs) != 2 {
			t.Fatalf("expected 2 configs, got %d", len(configs))
		}
		// docker gets true
		if configs[0].CRI != CRIDocker {
			t.Errorf("expected docker first, got %s", configs[0].CRI)
		}
		if configs[0].Registries[0] != "true" {
			t.Errorf("expected docker registries = [true], got %v", configs[0].Registries)
		}
		// containerd gets actual registries
		if configs[1].CRI != CRIContainerd {
			t.Errorf("expected containerd second, got %s", configs[1].CRI)
		}
		if len(configs[1].Registries) != 2 {
			t.Errorf("expected 2 registries for containerd, got %v", configs[1].Registries)
		}
	})

	t.Run("invalid mirror format errors", func(t *testing.T) {
		_, err := ResolveCRIConfigs([]string{"bad-format"}, false, nil, nil)
		if err == nil {
			t.Fatal("expected error for invalid mirror format")
		}
	})
}

func TestApplyCRIConfigs_UnsupportedCRI(t *testing.T) {
	configs := []CRIConfig{
		{CRI: CRIType("unknown"), Registries: []string{"docker.io"}},
	}
	results := ApplyCRIConfigs(configs, "localhost:8585")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Success {
		t.Error("expected failure for unsupported CRI")
	}
	if results[0].Error == "" {
		t.Error("expected non-empty error message")
	}
}

func TestApplyCRIConfigs_EmptyConfigs(t *testing.T) {
	results := ApplyCRIConfigs(nil, "localhost:8585")
	if len(results) != 0 {
		t.Fatalf("expected 0 results for nil configs, got %d", len(results))
	}
}
