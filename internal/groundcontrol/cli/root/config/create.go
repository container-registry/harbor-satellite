package config

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/spf13/cobra"
)

func NewCreateCommand(runtime *common.Runtime) *cobra.Command {
	var file string
	var request groundcontrol.ConfigCreateRequest
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Create a satellite configuration from a manifest",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			if err := common.ValidateRequired("file", file); err != nil {
				return err
			}
			var err error
			request, err = common.DecodeManifestFile[groundcontrol.ConfigCreateRequest](cmd, file)
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().CreateConfigWithResponse(cmd.Context(), request)
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "JSON or YAML request file, or - for stdin")
	common.MarkRequired(cmd, "file")
	return cmd
}
