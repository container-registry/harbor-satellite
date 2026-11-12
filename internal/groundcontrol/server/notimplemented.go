package server

import (
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// notImplemented answers routes that the spec declares but a skeleton
// change has not implemented yet.
func notImplemented(w http.ResponseWriter, item string) {
	WriteJSONError(w, notimpl.Err(item).Error(), http.StatusNotImplemented)
}
