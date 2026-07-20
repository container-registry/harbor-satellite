package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/root"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/root/auth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/root/config"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/root/group"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/root/satellite"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/root/spire"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/root/user"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const defaultServerURL = "https://localhost:8080/"

type flags struct {
	configFile string
	serverURL  string
	token      string
	timeout    time.Duration
	insecure   bool
}

func RootCmd() *cobra.Command {
	rootFlags := flags{}
	configuration := viper.New()
	configuration.SetEnvPrefix("GROUND_CONTROL")
	configuration.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	configuration.AutomaticEnv()
	configuration.SetDefault(common.URLKey, defaultServerURL)
	configuration.SetDefault(common.TimeoutKey, 30*time.Second)
	configuration.SetDefault(common.InsecureKey, false)

	runtime := common.NewRuntime(configuration)
	rootCmd := &cobra.Command{
		Use:   "groundcontrol",
		Short: "Manage a Harbor Ground Control service",
		Long: `groundcontrol is a command-line client for Harbor Ground Control.

Configuration precedence is command-line flags, environment variables,
configuration file, then defaults. General environment variables use the
GROUND_CONTROL_ prefix, for example GROUND_CONTROL_URL and
GROUND_CONTROL_TOKEN. Passwords are accepted only through the environment;
see the help for each password-using command.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			if rootFlags.configFile != "" {
				configuration.SetConfigFile(rootFlags.configFile)
				if err := configuration.ReadInConfig(); err != nil {
					return fmt.Errorf("read config file %q: %w", rootFlags.configFile, err)
				}
			}
			return runtime.Initialize()
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	rootCmd.PersistentFlags().StringVar(&rootFlags.configFile, "config", "", "configuration file")
	rootCmd.PersistentFlags().StringVar(&rootFlags.serverURL, "server", defaultServerURL, "Ground Control server URL")
	rootCmd.PersistentFlags().StringVar(&rootFlags.token, "token", "", "Ground Control bearer token")
	rootCmd.PersistentFlags().DurationVar(&rootFlags.timeout, "timeout", 30*time.Second, "HTTP request timeout")
	rootCmd.PersistentFlags().BoolVar(&rootFlags.insecure, "insecure", false, "skip HTTPS certificate verification")

	mustBindFlag(configuration.BindPFlag(common.URLKey, rootCmd.PersistentFlags().Lookup("server")))
	mustBindFlag(configuration.BindPFlag(common.TokenKey, rootCmd.PersistentFlags().Lookup("token")))
	mustBindFlag(configuration.BindPFlag(common.TimeoutKey, rootCmd.PersistentFlags().Lookup("timeout")))
	mustBindFlag(configuration.BindPFlag(common.InsecureKey, rootCmd.PersistentFlags().Lookup("insecure")))

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
		spire.NewAgentsCommand(runtime),
		spire.NewStatusCommand(runtime),
		user.NewListCommand(runtime),
		user.NewGetCommand(runtime),
	)
	createCmd := commandGroup("create", "Create Ground Control resources",
		config.NewCreateCommand(runtime),
		user.NewCreateCommand(runtime),
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
		satellite.NewDeleteCommand(runtime),
		user.NewDeleteCommand(runtime),
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

	rootCmd.AddCommand(
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

func mustBindFlag(err error) {
	if err != nil {
		panic(err)
	}
}
