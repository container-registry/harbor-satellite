package utils

import (
	"testing"
)

func TestGetRepositoryAndImageNameFromArtifact(t *testing.T) {
	tests := []struct {
		name          string
		repository    string
		expectedRepo  string
		expectedImage string
		expectError   bool
	}{
		{
			name:          "valid repo/image string",
			repository:    "library/ubuntu",
			expectedRepo:  "library",
			expectedImage: "ubuntu",
			expectError:   false,
		},
		{
			name:          "valid multi-segment repo/sub/image string",
			repository:    "myorg/subteam/app",
			expectedRepo:  "myorg",
			expectedImage: "subteam/app",
			expectError:   false,
		},
		{
			name:          "valid string with surrounding whitespace",
			repository:    "  library/ubuntu  ",
			expectedRepo:  "library",
			expectedImage: "ubuntu",
			expectError:   false,
		},
		{
			name:        "invalid single segment without slash",
			repository:  "ubuntu",
			expectError: true,
		},
		{
			name:        "invalid empty repo segment starting with slash",
			repository:  "/ubuntu",
			expectError: true,
		},
		{
			name:        "invalid empty image segment ending with slash",
			repository:  "library/",
			expectError: true,
		},
		{
			name:        "invalid slash only",
			repository:  "/",
			expectError: true,
		},
		{
			name:        "invalid double slash segment",
			repository:  "library//ubuntu",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, image, err := GetRepositoryAndImageNameFromArtifact(tt.repository)
			if tt.expectError {
				if err == nil {
					t.Errorf("GetRepositoryAndImageNameFromArtifact(%q) expected error, got nil", tt.repository)
				}
			} else {
				if err != nil {
					t.Errorf("GetRepositoryAndImageNameFromArtifact(%q) unexpected error: %v", tt.repository, err)
				}
				if repo != tt.expectedRepo {
					t.Errorf("GetRepositoryAndImageNameFromArtifact(%q) repo = %q, want %q", tt.repository, repo, tt.expectedRepo)
				}
				if image != tt.expectedImage {
					t.Errorf("GetRepositoryAndImageNameFromArtifact(%q) image = %q, want %q", tt.repository, image, tt.expectedImage)
				}
			}
		})
	}
}

func TestFormatRegistryURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "https prefix trimmed",
			input:    "https://registry.example.com",
			expected: "registry.example.com",
		},
		{
			name:     "http prefix trimmed",
			input:    "http://localhost:5000",
			expected: "localhost:5000",
		},
		{
			name:     "no prefix unchanged",
			input:    "harbor.local",
			expected: "harbor.local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatRegistryURL(tt.input); got != tt.expected {
				t.Errorf("FormatRegistryURL(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestHasInvalidPathChars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "clean path",
			input:    "valid/file/path.txt",
			expected: false,
		},
		{
			name:     "path with colon",
			input:    "invalid:path",
			expected: true,
		},
		{
			name:     "path with wildcard",
			input:    "path/*.txt",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasInvalidPathChars(tt.input); got != tt.expected {
				t.Errorf("HasInvalidPathChars(%q) = %t, want %t", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsValidURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid http url",
			input:    "http://example.com",
			expected: true,
		},
		{
			name:     "valid https url",
			input:    "https://harbor.example.com/api",
			expected: true,
		},
		{
			name:     "plain string without scheme",
			input:    "harbor.example.com",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidURL(tt.input); got != tt.expected {
				t.Errorf("IsValidURL(%q) = %t, want %t", tt.input, got, tt.expected)
			}
		})
	}
}
