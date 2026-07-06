package runtime

import (
	"fmt"
	"os"
	"testing"
)

func TestDetectWithCheckers(t *testing.T) {
	tests := []struct {
		name        string
		sockets     map[string]bool
		binaries    map[string]bool
		wantTypes   []CRIType
		wantReasons []string
	}{
		{
			name:      "detects docker via socket",
			sockets:   map[string]bool{"/var/run/docker.sock": true},
			wantTypes: []CRIType{CRIDocker},
		},
		{
			name:      "detects containerd via binary when no socket",
			binaries:  map[string]bool{"containerd": true},
			wantTypes: []CRIType{CRIContainerd},
		},
		{
			name:      "detects podman via binary only",
			binaries:  map[string]bool{"podman": true},
			wantTypes: []CRIType{CRIPodman},
		},
		{
			name:      "detects multiple CRIs",
			sockets:   map[string]bool{"/var/run/docker.sock": true, "/run/containerd/containerd.sock": true},
			wantTypes: []CRIType{CRIDocker, CRIContainerd},
		},
		{
			name:        "socket takes priority over binary",
			sockets:     map[string]bool{"/var/run/docker.sock": true},
			binaries:    map[string]bool{"docker": true},
			wantTypes:   []CRIType{CRIDocker},
			wantReasons: []string{"found socket /var/run/docker.sock"},
		},
		{
			name:      "no CRIs detected",
			wantTypes: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statFn := func(path string) (os.FileInfo, error) {
				if tt.sockets != nil && tt.sockets[path] {
					return nil, nil
				}
				return nil, fmt.Errorf("not found")
			}

			lookPathFn := func(name string) (string, error) {
				if tt.binaries != nil && tt.binaries[name] {
					return "/usr/bin/" + name, nil
				}
				return "", fmt.Errorf("not found")
			}

			got := detectWithCheckers(statFn, lookPathFn)

			if len(got) != len(tt.wantTypes) {
				t.Fatalf("detected %d CRIs, want %d: %v", len(got), len(tt.wantTypes), got)
			}

			for i, want := range tt.wantTypes {
				if got[i].Type != want {
					t.Errorf("detected[%d].Type = %q, want %q", i, got[i].Type, want)
				}
			}

			if tt.wantReasons != nil {
				for i, want := range tt.wantReasons {
					if got[i].Reason != want {
						t.Errorf("detected[%d].Reason = %q, want %q", i, got[i].Reason, want)
					}
				}
			}
		})
	}
}
