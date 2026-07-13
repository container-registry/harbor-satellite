package env

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDatabaseURLEscapesCredentials(t *testing.T) {
	t.Parallel()

	database := Database{
		Username: "service/user",
		Password: "p@ss:/?#[]",
		Host:     "db.example.test",
		Port:     "5432",
		Database: "ground-control",
	}

	parsed, err := url.Parse(database.URL())
	require.NoError(t, err)
	require.Equal(t, "service/user", parsed.User.Username())
	password, present := parsed.User.Password()
	require.True(t, present)
	require.Equal(t, "p@ss:/?#[]", password)
	require.Equal(t, "db.example.test:5432", parsed.Host)
	require.Equal(t, "/ground-control", parsed.Path)
	require.Equal(t, "disable", parsed.Query().Get("sslmode"))
}
