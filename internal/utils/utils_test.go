package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateRegistryAddress(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		port    string
		wantErr bool
		wantOut string
	}{
		{
			name:    "valid IPv4 and port",
			addr:    "192.168.1.1",
			port:    "5000",
			wantErr: false,
			wantOut: "192.168.1.1:5000",
		},
		{
			name:    "invalid IP address",
			addr:    "notanip",
			port:    "5000",
			wantErr: true,
		},
		{
			name:    "IPv6 address rejected",
			addr:    "::1",
			port:    "5000",
			wantErr: true,
		},
		{
			name:    "port too high",
			addr:    "192.168.1.1",
			port:    "99999",
			wantErr: true,
		},
		{
			name:    "port zero",
			addr:    "192.168.1.1",
			port:    "0",
			wantErr: true,
		},
		{
			name:    "port not a number",
			addr:    "192.168.1.1",
			port:    "abc",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ValidateRegistryAddress(tc.addr, tc.port)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantOut, out)
		})
	}
}

func TestIsValidURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid https URL", "https://registry.example.com", true},
		{"valid http URL", "http://registry.example.com", true},
		{"no scheme", "registry.example.com/repo:tag", false},
		{"empty string", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, IsValidURL(tc.input))
		})
	}
}

func TestHasInvalidPathChars(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"clean path", "/etc/config/file.json", false},
		{"backslash", "C:\\Users\\file", true},
		{"asterisk", "file*.json", true},
		{"question mark", "file?.json", true},
		{"double quote", "file\".json", true},
		{"angle brackets", "file<>.json", true},
		{"pipe", "file|.json", true},
		{"colon", "file:.json", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, HasInvalidPathChars(tc.input))
		})
	}
}

func TestGetRepositoryAndImageNameFromArtifact(t *testing.T) {
	tests := []struct {
		name       string
		repository string
		wantRepo   string
		wantImage  string
		wantErr    bool
	}{
		{
			name:       "simple repo/image",
			repository: "library/alpine",
			wantRepo:   "library",
			wantImage:  "alpine",
		},
		{
			name:       "nested repo/image/sub",
			repository: "project/service/alpine",
			wantRepo:   "project",
			wantImage:  "service/alpine",
		},
		{
			name:       "single part no slash",
			repository: "alpine",
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo, image, err := GetRepositoryAndImageNameFromArtifact(tc.repository)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantRepo, repo)
			require.Equal(t, tc.wantImage, image)
		})
	}
}

func TestFormatRegistryURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"strips https", "https://registry.example.com", "registry.example.com"},
		{"strips http", "http://registry.example.com", "registry.example.com"},
		{"bare URL unchanged", "registry.example.com", "registry.example.com"},
		{"empty string", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, FormatRegistryURL(tc.input))
		})
	}
}

func TestReadFileAndWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	data := []byte(`{"key":"value"}`)

	require.NoError(t, WriteFile(path, data))

	got, err := ReadFile(path, false)
	require.NoError(t, err)
	require.Equal(t, data, got)
}

func TestReadFile_NotFound(t *testing.T) {
	_, err := ReadFile("/nonexistent/path/file.json", false)
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))
}

func TestWriteFile_InvalidPath(t *testing.T) {
	err := WriteFile("/nonexistent/dir/file.json", []byte("data"))
	require.Error(t, err)
}
