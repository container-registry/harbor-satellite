package runtime

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewGenerateConfig(runtime string) *cobra.Command {
	generateConfig := &cobra.Command{
		Use:   "gen",
		Short: fmt.Sprintf("Generates the config file for the %s runtime", runtime),
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	return generateConfig
}
