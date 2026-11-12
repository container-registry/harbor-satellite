// Package policybundle is the edge admission policy that Ground Control
// delivers in app_config.admission and a satellite's proxy evaluates
// (plan item 7.5, ADR-0017): the Casbin model, the bundle format and the
// enforcement modes. Ground Control validates bundles against the same
// model the satellite runs.
package policybundle

import (
	_ "embed"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// ModelVersion names Model. A satellite refuses a bundle written for a
// model it does not have.
const ModelVersion = "edge-admission/v1"

// Model matches a request (sub, repo, ref, act) against policy lines
// (sub, repo, ref, act, eft): repo with keyMatch, ref with globMatch, "*"
// for any subject, reference or operation. Any allow and no deny admits.
// A blob request (ref "*") matches every allow of its repository, so a
// tag-scoped allow admits the image's blobs; only a deny on every
// reference denies them.
//
//go:embed model.conf
var Model string

// Mode is how the proxy applies the bundle.
type Mode string

const (
	// ModeOff does not evaluate the bundle.
	ModeOff Mode = "off"
	// ModeAudit evaluates and logs denials but serves the request.
	ModeAudit Mode = "audit"
	// ModeEnforce answers a denied request with the OCI error DENIED.
	ModeEnforce Mode = "enforce"
)

// Bundle is a set of Model policy lines. Each entry of Policies is one
// line [sub, repo, ref, act, eft], eft being "allow" or "deny"; act is a
// proxy operation name such as "pull_manifest".
type Bundle struct {
	ModelVersion string     `json:"model_version"`
	Policies     [][]string `json:"policies"`
}

// ParseMode parses an admission mode; "" is ModeOff, anything unknown an
// error.
func ParseMode(s string) (Mode, error) {
	return ModeOff, notimpl.Err("7.5 admission mode")
}

// Digest is "sha256:<hex>" of the bundle's canonical JSON with the policy
// lines sorted, so the order lines were written in does not change it. The
// proxy rebuilds its evaluator and drops its decision cache when it
// changes.
func (b Bundle) Digest() string {
	return ""
}
