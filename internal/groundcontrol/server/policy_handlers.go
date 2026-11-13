package server

import (
	"cmp"
	"log"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// The policy API (plan item 7.3) decodes, calls fleet.PolicyService and
// encodes; system admins only (requiresSystemAdmin).

func policyError(w http.ResponseWriter, op string, err error) {
	if StatusForError(err) == http.StatusInternalServerError {
		log.Printf("policy %s: %v", op, err)
	}
	handleFleetError(w, err, nil)
}

func (s *Server) ListPolicyRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.svc().Policies.Rules(r.Context())
	if err != nil {
		policyError(w, "list rules", err)
		return
	}
	out := make([]PolicyRule, 0, len(rules))
	for _, rule := range rules {
		out = append(out, toPolicyRule(rule))
	}
	WriteJSONResponse(w, http.StatusOK, out)
}

func (s *Server) CreatePolicyRule(w http.ResponseWriter, r *http.Request) {
	var req PolicyRuleRequest
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}
	rule, err := s.svc().Policies.AddRule(r.Context(), fleet.PolicyRule{
		Subject: req.Subject, Scope: cmp.Or(req.Scope, "*"), Type: cmp.Or(req.ResourceType, "*"),
		Action: cmp.Or(req.Action, "*"), Effect: string(req.Effect),
	})
	if err != nil {
		policyError(w, "create rule", err)
		return
	}
	WriteJSONResponse(w, http.StatusCreated, toPolicyRule(rule))
}

func (s *Server) DeletePolicyRule(w http.ResponseWriter, r *http.Request, id int64) {
	if err := s.svc().Policies.DeleteRule(r.Context(), id); err != nil {
		policyError(w, "delete rule", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ListRoleBindings(w http.ResponseWriter, r *http.Request) {
	bindings, err := s.svc().Policies.Bindings(r.Context())
	if err != nil {
		policyError(w, "list bindings", err)
		return
	}
	out := make([]RoleBinding, 0, len(bindings))
	for _, b := range bindings {
		out = append(out, RoleBinding{ID: b.ID, Principal: b.Principal, Role: b.Role})
	}
	WriteJSONResponse(w, http.StatusOK, out)
}

func (s *Server) CreateRoleBinding(w http.ResponseWriter, r *http.Request) {
	var req RoleBindingRequest
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}
	b, err := s.svc().Policies.AddBinding(r.Context(), fleet.RoleBinding{Principal: req.Principal, Role: req.Role})
	if err != nil {
		policyError(w, "create binding", err)
		return
	}
	WriteJSONResponse(w, http.StatusCreated, RoleBinding{ID: b.ID, Principal: b.Principal, Role: b.Role})
}

func (s *Server) DeleteRoleBinding(w http.ResponseWriter, r *http.Request, id int64) {
	if err := s.svc().Policies.DeleteBinding(r.Context(), id); err != nil {
		policyError(w, "delete binding", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toPolicyRule(r fleet.PolicyRule) PolicyRule {
	return PolicyRule{ID: r.ID, Subject: r.Subject, Scope: r.Scope, ResourceType: r.Type, Action: r.Action, Effect: r.Effect}
}
