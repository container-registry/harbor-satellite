package env

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var identityKeys = map[string]string{
	"OIDC_ENABLED":                 "true",
	"OIDC_ISSUER_URL":              "https://dex.example",
	"OIDC_CLIENT_ID":               "gc",
	"OIDC_CLIENT_SECRET":           "s3cret",
	"OIDC_REDIRECT_URL":            "https://gc.example/oidc/callback",
	"OIDC_SCOPES":                  "openid,groups",
	"OIDC_USERNAME_CLAIM":          "email",
	"OIDC_GROUPS_CLAIM":            "roles",
	"OIDC_ROLE_MAP":                "edge-ops=admin,platform=system_admin",
	"OIDC_DISABLE_PASSWORD_LOGIN":  "true",
	"OIDC_LOGIN_STATE_TTL":         "5m",
	"ROBOT_ROTATION_ENABLED":       "true",
	"ROBOT_ROTATION_BEFORE_EXPIRY": "48h",
	"ROBOT_ROTATION_INTERVAL":      "5m",
	"ROBOT_ROTATION_CACHE_REFRESH": "15s",
	"DATA_ENCRYPTION_KEY":          "a2V5",
}

func TestLoadGCParsesIdentityKeys(t *testing.T) {
	previous := GC
	t.Cleanup(func() { GC = previous })

	t.Run("defaults", func(t *testing.T) {
		for key := range identityKeys {
			t.Setenv(key, "")
			require.NoError(t, os.Unsetenv(key))
		}
		require.NoError(t, LoadGC())

		require.Equal(t, OIDC{
			Scopes:        []string{"openid", "profile", "email", "groups"},
			UsernameClaim: "preferred_username",
			GroupsClaim:   "groups",
			LoginStateTTL: 10 * time.Minute,
		}, GC.OIDC)
		require.Equal(t, RobotRotation{BeforeExpiry: 72 * time.Hour, Interval: 10 * time.Minute, CacheRefresh: 30 * time.Second}, GC.RobotRotation)
		require.Empty(t, GC.DataEncryptionKey)
	})

	t.Run("set", func(t *testing.T) {
		for key, value := range identityKeys {
			t.Setenv(key, value)
		}
		require.NoError(t, LoadGC())

		require.Equal(t, OIDC{
			Enabled: true, IssuerURL: "https://dex.example", ClientID: "gc", ClientSecret: "s3cret",
			RedirectURL: "https://gc.example/oidc/callback", Scopes: []string{"openid", "groups"},
			UsernameClaim: "email", GroupsClaim: "roles",
			RoleMap:              map[string]string{"edge-ops": "admin", "platform": "system_admin"},
			DisablePasswordLogin: true, LoginStateTTL: 5 * time.Minute,
		}, GC.OIDC)
		require.Equal(t, RobotRotation{Enabled: true, BeforeExpiry: 48 * time.Hour, Interval: 5 * time.Minute, CacheRefresh: 15 * time.Second}, GC.RobotRotation)
		require.Equal(t, "a2V5", GC.DataEncryptionKey)
	})
}
