package version_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	v "github.com/container-registry/harbor-satellite/internal/version"
)

func TestDefaults(t *testing.T) {
	if v.Version != "dev" {
		t.Errorf("expected default Version to be %q, got %q", "dev", v.Version)
	}
	if v.GitCommit != "unknown" {
		t.Errorf("expected default GitCommit to be %q, got %q", "unknown", v.GitCommit)
	}
}

func TestBuildStamping(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping build integration test in short mode")
	}

	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go binary not found in PATH")
	}

	// Create temp directory inside the module tree so Go allows importing internal packages
	tempDir, err := os.MkdirTemp(".", "testbuild_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	mainFile := filepath.Join(tempDir, "main.go")
	binFile := filepath.Join(tempDir, "version_test_bin")

	mainCode := `package main

import (
	"fmt"
	"github.com/container-registry/harbor-satellite/internal/version"
)

func main() {
	fmt.Printf("%s|%s\n", version.Version, version.GitCommit)
}
`

	if err := os.WriteFile(mainFile, []byte(mainCode), 0o600); err != nil {
		t.Fatalf("failed to write test main file: %v", err)
	}

	pkgPath := "github.com/container-registry/harbor-satellite/internal/version"
	expectedVersion := "v1.2.3"
	expectedCommit := "abc1234def5678"

	ldflags := "-X " + pkgPath + ".Version=" + expectedVersion + " -X " + pkgPath + ".GitCommit=" + expectedCommit

	cmd := exec.Command(goBin, "build", "-ldflags", ldflags, "-o", binFile, mainFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build test binary with ldflags: %v\nOutput: %s", err, string(output))
	}

	runCmd := exec.Command(binFile)
	runOutput, err := runCmd.Output()
	if err != nil {
		t.Fatalf("failed to run test binary: %v", err)
	}

	parts := strings.Split(strings.TrimSpace(string(runOutput)), "|")
	if len(parts) != 2 {
		t.Fatalf("unexpected output format: %q", string(runOutput))
	}

	gotVersion, gotCommit := parts[0], parts[1]

	if gotVersion != expectedVersion {
		t.Errorf("expected stamped Version %q, got %q", expectedVersion, gotVersion)
	}
	if gotCommit != expectedCommit {
		t.Errorf("expected stamped GitCommit %q, got %q", expectedCommit, gotCommit)
	}
}
