package migrator

import (
	"io/fs"
	"strings"
	"testing"

	migrations "github.com/container-registry/harbor-satellite/spec/groundcontrol/sql"
)

func TestEmbeddedMigrationsPresent(t *testing.T) {
	entries, err := fs.ReadDir(migrations.Schema, "schema")
	if err != nil {
		t.Fatalf("read embedded schema: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("embedded schema is empty")
	}

	found := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			t.Errorf("unexpected embedded entry %s", entry.Name())
		}
		found[entry.Name()] = struct{}{}
	}

	for _, name := range []string{"001_satellites.sql", "015_artifacts.sql"} {
		if _, ok := found[name]; !ok {
			t.Errorf("missing embedded migration %s", name)
		}
	}
}
