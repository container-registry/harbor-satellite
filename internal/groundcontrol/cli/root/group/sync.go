package group

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/spf13/cobra"
)

func NewSyncCommand(runtime *common.Runtime) *cobra.Command {
	var file string
	var request groundcontrol.GroupSyncRequest
	cmd := &cobra.Command{
		Use:   "group",
		Short: "Synchronize a group state artifact from a manifest",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			if err := common.ValidateRequired("file", file); err != nil {
				return err
			}
			var err error
			request, err = common.DecodeManifestFile[groundcontrol.GroupSyncRequest](cmd, file)
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().SyncGroupWithResponse(cmd.Context(), request)
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
