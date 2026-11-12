package migrator

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/stretchr/testify/require"
)

func TestValidateServer(t *testing.T) {
	tests := []struct {
		name       string
		version    string
		versionNum int
		walLevel   string
		wantErr    string
	}{
		{name: "19.0 replica", version: "19.0", versionNum: 190000, walLevel: "replica"},
		{name: "19rc1 replica", version: "19rc1", versionNum: 190000, walLevel: "replica"},
		{name: "19.2 logical", version: "19.2", versionNum: 190002, walLevel: "logical"},
		{name: "20.0", version: "20.0", versionNum: 200000, walLevel: "replica"},
		{name: "18.4 refused with its version", version: "18.4", versionNum: 180004, walLevel: "replica", wantErr: "PostgreSQL 18.4 is not supported"},
		{name: "17.11 refused", version: "17.11", versionNum: 170011, walLevel: "logical", wantErr: "PostgreSQL 17.11 is not supported"},
		{name: "minimal wal refused", version: "19.0", versionNum: 190000, walLevel: "minimal", wantErr: "wal_level=minimal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateServer(tt.version, tt.versionNum, tt.walLevel)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestCheckServerAcceptsTheIntegrationServer(t *testing.T) {
	db := dbtest.New(t)
	require.NoError(t, CheckServer(context.Background(), db.DB))
}
