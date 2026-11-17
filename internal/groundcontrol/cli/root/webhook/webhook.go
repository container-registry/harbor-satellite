package webhook

import (
	"fmt"
	"strings"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/spf13/cobra"
)

const secretEnv = "GROUND_CONTROL_WEBHOOK_SECRET"

func NewListCommand(runtime *common.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "webhooks",
		Short: "List webhook subscriptions and their delivery state",
		Long: `List webhook subscriptions with their delivery state: enabled,
consecutive_failures, last_error and disabled_reason.`,
		Args:    cobra.NoArgs,
		PreRunE: common.RequiredAuth(runtime),
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().ListWebhooksWithResponse(cmd.Context())
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
}

func NewCreateCommand(runtime *common.Runtime) *cobra.Command {
	var request groundcontrol.WebhookSubscriptionRequest
	cmd := &cobra.Command{
		Use:   "webhook",
		Short: "Subscribe a receiver to fleet events",
		Long: `Subscribe a receiver to fleet events. The HMAC-SHA256 signing secret, at
least 32 bytes, is read from ` + secretEnv + `. Without --event-type the
receiver gets every kind. Delivery starts with events committed after the
subscription.`,
		Args: cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			if err := common.ValidateRequired("url", request.URL); err != nil {
				return err
			}
			for _, kind := range request.EventTypes {
				if strings.TrimSpace(kind) == "" {
					return fmt.Errorf("--event-type must not be empty")
				}
			}
			var err error
			request.Secret, err = common.RequiredEnv(secretEnv)
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().CreateWebhookWithResponse(cmd.Context(), request)
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().StringVar(&request.URL, "url", "", "receiver URL: https, or http for a loopback host")
	cmd.Flags().StringSliceVar(&request.EventTypes, "event-type", nil, "event kind to deliver; repeatable or comma separated")
	common.MarkRequired(cmd, "url")
	return cmd
}

func NewDeleteCommand(runtime *common.Runtime) *cobra.Command {
	var id int32
	cmd := &cobra.Command{
		Use:   "webhook",
		Short: "Delete a webhook subscription and its delivery checkpoint",
		Args:  cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			if id <= 0 {
				return fmt.Errorf("--id must be a positive number")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().DeleteWebhookWithResponse(cmd.Context(), id)
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().Int32Var(&id, "id", 0, "webhook subscription ID")
	common.MarkRequired(cmd, "id")
	return cmd
}
