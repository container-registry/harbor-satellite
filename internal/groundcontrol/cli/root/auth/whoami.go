package auth

import (
	"fmt"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/spf13/cobra"
)

func NewWhoamiCommand(runtime *common.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the stored Ground Control session",
		Long: `Print the server URL, username and expiry of the session saved for the
configured --server. It exits non-zero when no session is stored.`,
		Args:        cobra.NoArgs,
		Annotations: map[string]string{common.KeepStoredSessionAnnotation: "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			session, ok, err := runtime.StoredSession()
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("no session stored for the configured server: run auth login")
			}

			out := cmd.OutOrStdout()
			username := session.Username
			if username == "" {
				username = "(unknown)"
			}
			fmt.Fprintf(out, "Server:   %s\n", session.Server)
			fmt.Fprintf(out, "Username: %s\n", username)
			switch {
			case session.ExpiresAt.IsZero():
				fmt.Fprintln(out, "Expires:  (unknown)")
			case session.Expired:
				fmt.Fprintf(out, "Expires:  %s (expired)\n", session.ExpiresAt.UTC().Format(time.RFC3339))
			default:
				fmt.Fprintf(out, "Expires:  %s\n", session.ExpiresAt.UTC().Format(time.RFC3339))
			}
			return nil
		},
	}
}
