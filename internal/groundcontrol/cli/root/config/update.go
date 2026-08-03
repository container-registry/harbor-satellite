package config

import (
	"bytes"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/spf13/cobra"
)

func NewUpdateCommand(runtime *common.Runtime) *cobra.Command {
	var name string
	var file string
	var request []byte
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Apply a merge-patch manifest to a satellite configuration",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			if err := common.ValidateRequired("name", name, "file", file); err != nil {
				return err
			}
			var err error
			request, err = common.DecodeManifestJSONFile(cmd, file)
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().UpdateConfigWithBodyWithResponse(
				cmd.Context(),
				name,
				"application/json",
				bytes.NewReader(request),
			)
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "configuration name")
	cmd.Flags().StringVarP(&file, "file", "f", "", "JSON or YAML merge-patch file, or - for stdin")
	common.MarkRequired(cmd, "name", "file")
	return cmd
}
