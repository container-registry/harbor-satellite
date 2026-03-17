package runtime

import (
	"fmt"
	"os"
	"testing"
)

func TestDetectImageDir(t *testing.T) {
	tests := []struct {
		name string
		dirs map[string]bool // path -> isDir
		want string
	}{
		{
			name: "detects k3s image dir",
			dirs: map[string]bool{"/var/lib/rancher/k3s/agent/images": true},
			want: "/var/lib/rancher/k3s/agent/images",
		},
		{
			name: "detects rke2 image dir",
			dirs: map[string]bool{"/var/lib/rancher/rke2/agent/images": true},
			want: "/var/lib/rancher/rke2/agent/images",
		},
		{
			name: "prefers k3s over rke2",
			dirs: map[string]bool{
				"/var/lib/rancher/k3s/agent/images":  true,
				"/var/lib/rancher/rke2/agent/images": true,
			},
			want: "/var/lib/rancher/k3s/agent/images",
		},
		{
			name: "returns empty when none found",
			dirs: nil,
			want: "",
		},
		{
			name: "ignores path that is a file not dir",
			dirs: map[string]bool{"/var/lib/rancher/k3s/agent/images": false},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stat := func(path string) (os.FileInfo, error) {
				isDir, ok := tt.dirs[path]
				if !ok {
					return nil, fmt.Errorf("not found")
				}
				return fakeFileInfo{dir: isDir}, nil
			}

			got := detectImageDir(stat)
			if got != tt.want {
				t.Errorf("detectImageDir() = %q, want %q", got, tt.want)
			}
		})
	}
}

// fakeFileInfo implements os.FileInfo for testing.
type fakeFileInfo struct {
	dir bool
	os.FileInfo
}

func (f fakeFileInfo) IsDir() bool { return f.dir }
