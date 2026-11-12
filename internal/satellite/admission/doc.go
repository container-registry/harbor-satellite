// Package admission evaluates Ground Control's edge admission bundle
// (internal/shared/policybundle) for every request of the satellite's OCI
// proxy (plan item 7.5, ADR-0017, superseding ADR-0009's CEL section). The
// bundle travels in app_config.admission of the config state artifact, so
// it reaches a satellite with its config and needs no second artifact.
// Nothing composes the middleware yet; cmd/satellite is unchanged.
package admission
