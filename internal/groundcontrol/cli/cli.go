package cli

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/root"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/root/auth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/root/config"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/root/group"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/root/outbox"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/root/policy"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/root/satellite"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/root/spire"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/root/user"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/root/webhook"
	"github.com/spf13/cobra"
)

func RootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "groundcontrol",
		Short: "Manage a Harbor Ground Control service",
		Long: `groundcontrol is a command-line client for Harbor Ground Control.

Configuration precedence is command-line flags, environment variables,
configuration file, saved login credentials, then defaults. General
environment variables use the GROUND_CONTROL_ prefix, for example
GROUND_CONTROL_URL and GROUND_CONTROL_TOKEN. Passwords are accepted only
through the environment; see the help for each password-using command.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	runtime := common.Load(rootCmd)

	authCmd := commandGroup("auth", "Manage Ground Control sessions",
		auth.NewLoginCommand(runtime),
		auth.NewLogoutCommand(runtime),
	)
	getCmd := commandGroup("get", "Get Ground Control resources",
		config.NewListCommand(runtime),
		config.NewGetCommand(runtime),
		group.NewListCommand(runtime),
		group.NewGetCommand(runtime),
		group.NewSatellitesCommand(runtime),
		satellite.NewListCommand(runtime),
		satellite.NewGetCommand(runtime),
		satellite.NewActiveCommand(runtime),
		satellite.NewStaleCommand(runtime),
		satellite.NewCachedImagesCommand(runtime),
		satellite.NewStatusCommand(runtime),
		satellite.NewLifecycleCommand(runtime),
		policy.NewListCommand(runtime),
		policy.NewListRoleBindingsCommand(runtime),
		webhook.NewListCommand(runtime),
		outbox.NewListCommand(runtime),
		spire.NewAgentsCommand(runtime),
		spire.NewStatusCommand(runtime),
		user.NewListCommand(runtime),
		user.NewGetCommand(runtime),
	)
	createCmd := commandGroup("create", "Create Ground Control resources",
		config.NewCreateCommand(runtime),
		policy.NewCreateCommand(runtime),
		policy.NewCreateRoleBindingCommand(runtime),
		user.NewCreateCommand(runtime),
		webhook.NewCreateCommand(runtime),
	)
	updateCmd := commandGroup("update", "Update Ground Control resources",
		config.NewUpdateCommand(runtime),
		satellite.NewUpdateConfigCommand(runtime),
		user.NewUpdateOwnPasswordCommand(runtime),
		user.NewUpdatePasswordCommand(runtime),
	)
	deleteCmd := commandGroup("delete", "Delete Ground Control resources",
		config.NewDeleteCommand(runtime),
		group.NewDeleteCommand(runtime),
		policy.NewDeleteCommand(runtime),
		policy.NewDeleteRoleBindingCommand(runtime),
		satellite.NewDeleteCommand(runtime),
		user.NewDeleteCommand(runtime),
		webhook.NewDeleteCommand(runtime),
	)
	addCmd := commandGroup("add", "Add Ground Control resource relationships",
		group.NewAddSatelliteCommand(runtime),
	)
	removeCmd := commandGroup("remove", "Remove Ground Control resource relationships",
		group.NewRemoveSatelliteCommand(runtime),
	)
	syncCmd := commandGroup("sync", "Synchronize Ground Control resources",
		group.NewSyncCommand(runtime),
	)
	registerCmd := commandGroup("register", "Register Ground Control resources",
		satellite.NewRegisterCommand(runtime),
		satellite.NewRegisterSpiffeCommand(runtime),
	)
	revokeCmd := commandGroup("revoke", "Revoke Ground Control resources",
		satellite.NewRevokeCommand(runtime),
	)
	unrevokeCmd := commandGroup("unrevoke", "Lift the revocation of Ground Control resources",
		satellite.NewUnrevokeCommand(runtime),
	)

	rootCmd.AddCommand(
		root.ServeCommand(),
		root.WorkerCommand(),
		authCmd,
		root.HealthCommand(runtime),
		root.PingCommand(runtime),
		getCmd,
		createCmd,
		updateCmd,
		deleteCmd,
		addCmd,
		removeCmd,
		syncCmd,
		registerCmd,
		revokeCmd,
		unrevokeCmd,
	)

	return rootCmd
}

func commandGroup(use string, short string, commands ...*cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(commands...)
	return cmd
}
