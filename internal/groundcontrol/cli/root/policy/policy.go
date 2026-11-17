package policy

import (
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/spf13/cobra"
)

func NewListCommand(runtime *common.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:     "policies",
		Short:   "List stored policy rules",
		Args:    cobra.NoArgs,
		PreRunE: common.RequiredAuth(runtime),
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().ListPolicyRulesWithResponse(cmd.Context())
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
}

func NewCreateCommand(runtime *common.Runtime) *cobra.Command {
	var request groundcontrol.PolicyRuleRequest
	var effect string
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Create a policy rule",
		Long: `Create a policy rule. --subject is user:<name>, role:<role> or oidc:<group>;
--scope, --resource-type and --action default to "*" on the server.`,
		Args: cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			if err := common.ValidateRequired("subject", request.Subject); err != nil {
				return err
			}
			switch groundcontrol.PolicyRuleRequestEffect(effect) {
			case groundcontrol.Allow, groundcontrol.Deny:
				request.Effect = groundcontrol.PolicyRuleRequestEffect(effect)
				return nil
			default:
				return fmt.Errorf("--effect must be %s or %s", groundcontrol.Allow, groundcontrol.Deny)
			}
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().CreatePolicyRuleWithResponse(cmd.Context(), request)
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().StringVar(&request.Subject, "subject", "", "user:<name>, role:<role> or oidc:<group>")
	cmd.Flags().StringVar(&effect, "effect", "", "allow or deny")
	cmd.Flags().StringVar(&request.Scope, "scope", "", `"*" or <satellite|group|config>:<name>`)
	cmd.Flags().StringVar(&request.ResourceType, "resource-type", "", `resource type or "*"`)
	cmd.Flags().StringVar(&request.Action, "action", "", `action or "*"`)
	common.MarkRequired(cmd, "subject", "effect")
	return cmd
}

func NewDeleteCommand(runtime *common.Runtime) *cobra.Command {
	var id int64
	cmd := &cobra.Command{
		Use:     "policy",
		Short:   "Delete a policy rule",
		Args:    cobra.NoArgs,
		PreRunE: requiredID(runtime, &id),
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().DeletePolicyRuleWithResponse(cmd.Context(), id)
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().Int64Var(&id, "id", 0, "policy rule ID")
	common.MarkRequired(cmd, "id")
	return cmd
}

func NewListRoleBindingsCommand(runtime *common.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:     "role-bindings",
		Short:   "List role bindings",
		Args:    cobra.NoArgs,
		PreRunE: common.RequiredAuth(runtime),
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().ListRoleBindingsWithResponse(cmd.Context())
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
}

func NewCreateRoleBindingCommand(runtime *common.Runtime) *cobra.Command {
	var request groundcontrol.RoleBindingRequest
	cmd := &cobra.Command{
		Use:   "role-binding",
		Short: "Give a principal an additional role",
		Long:  "Give a principal (user:<name> or oidc:<group>) an additional role (role:<name>, not a built-in role).",
		Args:  cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			return common.ValidateRequired("principal", request.Principal, "role", request.Role)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().CreateRoleBindingWithResponse(cmd.Context(), request)
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().StringVar(&request.Principal, "principal", "", "user:<name> or oidc:<group>")
	cmd.Flags().StringVar(&request.Role, "role", "", "role:<name>")
	common.MarkRequired(cmd, "principal", "role")
	return cmd
}

func NewDeleteRoleBindingCommand(runtime *common.Runtime) *cobra.Command {
	var id int64
	cmd := &cobra.Command{
		Use:     "role-binding",
		Short:   "Delete a role binding",
		Args:    cobra.NoArgs,
		PreRunE: requiredID(runtime, &id),
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().DeleteRoleBindingWithResponse(cmd.Context(), id)
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().Int64Var(&id, "id", 0, "role binding ID")
	common.MarkRequired(cmd, "id")
	return cmd
}

func requiredID(runtime *common.Runtime, id *int64) func(*cobra.Command, []string) error {
	return func(_ *cobra.Command, _ []string) error {
		if err := runtime.ValidateAuth(); err != nil {
			return err
		}
		if *id <= 0 {
			return fmt.Errorf("--id must be a positive number")
		}
		return nil
	}
}
