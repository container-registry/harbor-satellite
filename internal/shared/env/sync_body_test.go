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

func TestLoadGCParsesAPIBodyLimits(t *testing.T) {
	if err := LoadGC(); err != nil {
		t.Fatalf("LoadGC() error = %v", err)
	}
	if GC.Server.APIMaxBodyBytes != 1<<20 || GC.Server.GroupSyncMaxBodyBytes != 16<<20 {
		t.Fatalf("defaults API_MAX_BODY_BYTES = %d, GROUP_SYNC_MAX_BODY_BYTES = %d, want 1 MiB and 16 MiB",
			GC.Server.APIMaxBodyBytes, GC.Server.GroupSyncMaxBodyBytes)
	}

	t.Setenv("API_MAX_BODY_BYTES", "2048")
	t.Setenv("GROUP_SYNC_MAX_BODY_BYTES", "0")
	if err := LoadGC(); err != nil {
		t.Fatalf("LoadGC() error = %v", err)
	}
	if GC.Server.APIMaxBodyBytes != 2048 || GC.Server.GroupSyncMaxBodyBytes != 0 {
		t.Fatalf("API_MAX_BODY_BYTES = %d, GROUP_SYNC_MAX_BODY_BYTES = %d, want 2048 and 0",
			GC.Server.APIMaxBodyBytes, GC.Server.GroupSyncMaxBodyBytes)
	}
}
