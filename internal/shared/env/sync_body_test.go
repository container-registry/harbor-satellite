package env

import "testing"

func TestLoadGCParsesSyncMaxBodyBytes(t *testing.T) {
	if err := LoadGC(); err != nil {
		t.Fatalf("LoadGC() error = %v", err)
	}
	if GC.Server.SyncMaxBodyBytes != 4<<20 {
		t.Fatalf("SYNC_MAX_BODY_BYTES default = %d, want 4 MiB", GC.Server.SyncMaxBodyBytes)
	}

	t.Setenv("SYNC_MAX_BODY_BYTES", "1024")
	if err := LoadGC(); err != nil {
		t.Fatalf("LoadGC() error = %v", err)
	}
	if GC.Server.SyncMaxBodyBytes != 1024 {
		t.Fatalf("SYNC_MAX_BODY_BYTES = %d, want 1024", GC.Server.SyncMaxBodyBytes)
	}
}
