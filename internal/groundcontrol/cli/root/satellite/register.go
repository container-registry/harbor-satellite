package satellite

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/spf13/cobra"
)

func NewRegisterCommand(runtime *common.Runtime) *cobra.Command {
	var name string
	var configName string
	var groups []string
	cmd := &cobra.Command{
		Use:   "satellite",
		Short: "Register a token-managed satellite",
		Args:  cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			return common.ValidateRequired("name", name, "config", configName)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			request := groundcontrol.TokenSatelliteRegistrationRequest{
				Name:       name,
				ConfigName: configName,
			}
			if len(groups) > 0 {
				request.Groups = &groups
			}
			response, err := runtime.Client().RegisterSatelliteWithResponse(cmd.Context(), request)
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "satellite name")
	cmd.Flags().StringVar(&configName, "config", "", "configuration name")
	cmd.Flags().StringSliceVar(&groups, "group", nil, "group membership (repeatable)")
	common.MarkRequired(cmd, "name", "config")
	return cmd
}

func NewRegisterSpiffeCommand(runtime *common.Runtime) *cobra.Command {
	var file string
	var request groundcontrol.SPIFFESatelliteRegistrationRequest
	cmd := &cobra.Command{
		Use:   "spiffe-satellite",
		Short: "Register a SPIFFE satellite from a manifest",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			if err := common.ValidateRequired("file", file); err != nil {
				return err
			}
			var err error
			request, err = common.DecodeManifestFile[groundcontrol.SPIFFESatelliteRegistrationRequest](cmd, file)
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().RegisterSatelliteWithSpiffeWithResponse(cmd.Context(), request)
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
