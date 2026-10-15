package env

import (
	"net/url"
	"os"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

func testDatabase() Database {
	return Database{Host: "db.example", Port: "5432", Username: "gc", Password: "secret", Database: "groundcontrol", SSLMode: "disable"}
}

func TestDatabaseConnectionURL_EscapesCredentials(t *testing.T) {
	pending.Plan(t, "3.3", "URL() formats the DSN with Sprintf and does not escape credentials")
	d := testDatabase()
	d.Username = "gc@edge"
	d.Password = "p@ss:w/rd#"

	raw, err := d.ConnectionURL()
	require.NoError(t, err)
	u, err := url.Parse(raw)
	require.NoError(t, err)
	password, _ := u.User.Password()
	require.Equal(t, "gc@edge", u.User.Username())
	require.Equal(t, "p@ss:w/rd#", password)
	require.Equal(t, "db.example:5432", u.Host)
	require.Equal(t, "/groundcontrol", u.Path)
}

func TestDatabaseConnectionURL_SSLModeAndRootCert(t *testing.T) {
	pending.Plan(t, "3.3", "the DSN hard-codes sslmode=disable")
	d := testDatabase()
	d.SSLMode = "verify-full"
	d.SSLRootCert = "/ca.pem"

	raw, err := d.ConnectionURL()
	require.NoError(t, err)
	u, err := url.Parse(raw)
	require.NoError(t, err)
	require.Equal(t, "sslmode=verify-full&sslrootcert=%2Fca.pem", u.RawQuery)
}

func TestDatabaseValidate_RejectsUnknownSSLMode(t *testing.T) {
	pending.Plan(t, "3.3", "DB_SSLMODE is not validated")
	for _, mode := range []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"} {
		d := testDatabase()
		d.SSLMode = mode
		require.NoError(t, d.Validate(), mode)
	}

	d := testDatabase()
	d.SSLMode = "verify"
	err := d.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "verify")
}

func TestDatabaseSSLModeDefaultsToRequire(t *testing.T) {
	pending.Plan(t, "3.3", "G5: the default sslmode is still disable")
	t.Setenv("DB_SSLMODE", "")
	require.NoError(t, os.Unsetenv("DB_SSLMODE"))
	previous := GC
	t.Cleanup(func() { GC = previous })

	require.NoError(t, LoadGC())
	require.Equal(t, "require", GC.Database.SSLMode)
}
