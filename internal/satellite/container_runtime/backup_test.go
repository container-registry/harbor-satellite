package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupFile(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		missing   bool
		wantEmpty bool
		wantErr   bool
	}{
		{
			name:    "backs up existing file",
			content: `{"key": "value"}`,
		},
		{
			name:      "no-op for missing file",
			missing:   true,
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "test.json")

			if !tt.missing {
				if err := os.WriteFile(path, []byte(tt.content), 0600); err != nil {
					t.Fatal(err)
				}
			}

			backupPath, err := backupFile(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("backupFile() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantEmpty {
				if backupPath != "" {
					t.Fatalf("expected empty backup path, got %q", backupPath)
				}
				return
			}

			if backupPath == "" {
				t.Fatal("expected non-empty backup path")
			}

			got, err := os.ReadFile(backupPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.content {
				t.Fatalf("backup content = %q, want %q", got, tt.content)
			}
		})
	}
}

func TestValidateJSON(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr bool
	}{
		{"valid json", `{"key":"val"}`, false},
		{"valid empty", `{}`, false},
		{"invalid json", `{bad`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateJSON([]byte(tt.data)); (err != nil) != tt.wantErr {
				t.Fatalf("validateJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateTOML(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr bool
	}{
		{"valid toml", `key = "val"`, false},
		{"valid section", "[section]\nkey = \"val\"", false},
		{"invalid toml", `[bad`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateTOML([]byte(tt.data)); (err != nil) != tt.wantErr {
				t.Fatalf("validateTOML() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRestoreBackup(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "config.json")
	backup := filepath.Join(dir, "config.json.bak")

	originalContent := `{"original": true}`
	backupContent := `{"backup": true}`

	if err := os.WriteFile(original, []byte(originalContent), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte(backupContent), 0600); err != nil {
		t.Fatal(err)
	}

	if err := restoreBackup(backup, original); err != nil {
		t.Fatalf("restoreBackup() error = %v", err)
	}

	got, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != backupContent {
		t.Fatalf("restored content = %q, want %q", got, backupContent)
	}
}
