package server

import "net/http"

// The policy API (plan item 7.3) decodes, calls fleet.PolicyService and
// encodes; system admins only (requiresSystemAdmin).

func (s *Server) ListPolicyRules(w http.ResponseWriter, _ *http.Request) {
	notImplemented(w, "7.3 list policy rules")
}

func (s *Server) CreatePolicyRule(w http.ResponseWriter, _ *http.Request) {
	notImplemented(w, "7.3 create policy rule")
}

func (s *Server) DeletePolicyRule(w http.ResponseWriter, _ *http.Request, _ int64) {
	notImplemented(w, "7.3 delete policy rule")
}

func (s *Server) ListRoleBindings(w http.ResponseWriter, _ *http.Request) {
	notImplemented(w, "7.3 list role bindings")
}

func (s *Server) CreateRoleBinding(w http.ResponseWriter, _ *http.Request) {
	notImplemented(w, "7.3 create role binding")
}

func (s *Server) DeleteRoleBinding(w http.ResponseWriter, _ *http.Request, _ int64) {
	notImplemented(w, "7.3 delete role binding")
}
