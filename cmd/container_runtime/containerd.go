package runtime

import (

	containerd "github.com/containerd/containerd/pkg/cri/config"
	"github.com/spf13/cobra"
)

func NewContainerdCommand() *cobra.Command {
	containerd := &cobra.Command{
		Use:   "containerd",
		Short: "Creates the config file for the containerd runtime to fetch the images from the local repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Step 1: Generate a default config for the containerd
			config := containerd.Config{}
			// Step 2: We have generate the default plugin config for the containerd config
			config.PluginConfig = containerd.DefaultConfig()
			// Step 3: Now that we have the default plugin config, we need to generate the necessary registry config
			return nil
		},
	}
	containerd.AddCommand(NewReadConfigCommand("containerd"))
	return containerd
}
