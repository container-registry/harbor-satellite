package edgejournal_test

import (
	"encoding/json"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/edgejournal"
	"github.com/stretchr/testify/require"
)

// The payloads are a wire contract between satellites and Ground Control
// and, through edge events, the public event catalogue; this freezes their
// JSON.
func TestPayloads_JSON(t *testing.T) {
	for name, tc := range map[string]struct {
		payload any
		want    string
	}{
		"replication": {
			edgejournal.Replication{
				SchemaVersion: edgejournal.SchemaVersion, CycleID: "c-1", Action: edgejournal.ActionReplicated, Group: "g1",
				Repository: "library/alpine", Tag: "3.20", Digest: "sha256:aaa", Bytes: 2048, DurationMs: 15,
			},
			`{"schema_version":"1","cycle_id":"c-1","action":"replicated","group":"g1","repository":"library/alpine","tag":"3.20","digest":"sha256:aaa","bytes":2048,"duration_ms":15}`,
		},
		"failed deletion": {
			edgejournal.Replication{
				SchemaVersion: edgejournal.SchemaVersion, Action: edgejournal.ActionDeleted, Group: "g1",
				Repository: "library/alpine", Tag: "3.19", Digest: "sha256:bbb", Error: "permission denied",
			},
			`{"schema_version":"1","action":"deleted","group":"g1","repository":"library/alpine","tag":"3.19","digest":"sha256:bbb","error":"permission denied"}`,
		},
		"cri": {
			edgejournal.CRI{SchemaVersion: edgejournal.SchemaVersion, Runtime: "containerd", Success: true, BackupPath: "/etc/containerd/certs.d.bak"},
			`{"schema_version":"1","runtime":"containerd","success":true,"backup_path":"/etc/containerd/certs.d.bak"}`,
		},
		"failed cri": {
			edgejournal.CRI{SchemaVersion: edgejournal.SchemaVersion, Runtime: "docker", Error: "restart failed"},
			`{"schema_version":"1","runtime":"docker","success":false,"error":"restart failed"}`,
		},
		"pull-through": {
			edgejournal.PullThrough{SchemaVersion: edgejournal.SchemaVersion, Repository: "library/alpine", Reference: "3.20", Digest: "sha256:aaa", Bytes: 4096, DurationMs: 30},
			`{"schema_version":"1","repository":"library/alpine","reference":"3.20","digest":"sha256:aaa","bytes":4096,"duration_ms":30}`,
		},
		"dropped": {
			edgejournal.Dropped{SchemaVersion: edgejournal.SchemaVersion, Count: 8},
			`{"schema_version":"1","count":8}`,
		},
	} {
		got, err := json.Marshal(tc.payload)
		require.NoError(t, err, name)
		require.Equal(t, tc.want, string(got), name)
	}
}

func TestRecordTypes_ListsEveryType(t *testing.T) {
	require.Equal(t, []edgejournal.RecordType{
		edgejournal.RecordReplication, edgejournal.RecordCRI, edgejournal.RecordPullThrough, edgejournal.RecordDropped,
	}, edgejournal.RecordTypes())
}
