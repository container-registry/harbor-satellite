package runtime

// import (
// 	"fmt"

// 	"github.com/container-registry/harbor-satellite/internal/logger"
// 	"github.com/container-registry/harbor-satellite/internal/utils"
// 	"github.com/spf13/cobra"
// )

// func NewReadConfigCommand(runtime string) *cobra.Command {
// 	readContainerdConfig := &cobra.Command{
// 		Use:   "read",
// 		Short: fmt.Sprintf("Reads the config file for the %s runtime", runtime),
// 		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
// 			return fmt.Errorf("") // just to pass linter
// 		},
// 		RunE: func(cmd *cobra.Command, args []string) error {
// 			//Parse the flags
// 			path, err := cmd.Flags().GetString("path")
// 			if err != nil {
// 				return fmt.Errorf("error reading the path flag: %v", err)
// 			}
// 			log := logger.FromContext(cmd.Context())
// 			log.Info().Msgf("Reading the containerd config file from path: %s", path)
// 			_, err = utils.ReadFile(path, true)
// 			if err != nil {
// 				return fmt.Errorf("error reading the containerd config file: %v", err)
// 			}
// 			return nil
// 		},
// 	}
// 	return readContainerdConfig
// }
