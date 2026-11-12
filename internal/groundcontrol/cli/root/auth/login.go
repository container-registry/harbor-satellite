package auth

import (
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/spf13/cobra"
)

func NewLoginCommand(runtime *common.Runtime) *cobra.Command {
	var username string
	var password string
	var useOIDC bool
	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "Create a Ground Control session",
		Long: `Create a Ground Control session and save it in the OS user configuration
directory. A password login needs --username and the GROUND_CONTROL_PASSWORD
environment variable. --oidc logs in through the OIDC device flow instead:
it prints a URL and a code to confirm at the identity provider and needs no
password; --username then only names the stored session (default "oidc").`,
		Args: cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if useOIDC {
				return nil
			}
			if err := common.ValidateRequired("username", username); err != nil {
				return err
			}
			var err error
			password, err = common.RequiredEnv("GROUND_CONTROL_PASSWORD")
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if useOIDC {
				session, err := deviceLogin(cmd.Context(), runtime.Client(), cmd.OutOrStdout(), sleepCtx)
				if err != nil {
					return err
				}
				name := username
				if name == "" {
					name = "oidc"
				}
				return runtime.StoreToken(name, session.Token, session.ExpiresAt)
			}
			response, err := runtime.Client().LoginWithResponse(cmd.Context(), groundcontrol.LoginRequest{
				Username: username,
				Password: password,
			})
			if err != nil {
				return err
			}
			if err := common.ResponseError(response); err != nil {
				return err
			}
			if response.JSON200 == nil {
				return fmt.Errorf("login response did not contain a session")
			}
			return runtime.StoreToken(username, response.JSON200.Token, response.JSON200.ExpiresAt)
		},
	}
	loginCmd.Flags().StringVar(&username, "username", "", "Ground Control username")
	loginCmd.Flags().BoolVar(&useOIDC, "oidc", false, "Log in through the OIDC device flow; --username only names the stored session")
	return loginCmd
}
