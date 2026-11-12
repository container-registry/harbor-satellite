package async

import (
	"context"
	"errors"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// ErrNoHandler is returned for an event kind nobody registered; the
// publisher logs and completes such an event instead of retrying it.
var ErrNoHandler = errors.New("no handler for event kind")

type Handler interface {
	Handle(ctx context.Context, event OutboxEvent) error
}

type HandlerFunc func(ctx context.Context, event OutboxEvent) error

func (f HandlerFunc) Handle(ctx context.Context, event OutboxEvent) error { return f(ctx, event) }

// RoutedHandler names the event kinds it handles.
type RoutedHandler interface {
	Handler
	Kinds() []fleet.EventKind
}

// Router dispatches an event to every handler registered for its kind, in
// registration order.
type Router struct{}

func NewRouter() *Router { return &Router{} }

func (r *Router) Register(kind fleet.EventKind, h Handler) error {
	return notimpl.Err("2.3 router register")
}

func (r *Router) Handle(ctx context.Context, event OutboxEvent) error {
	return notimpl.Err("2.3 router")
}

// NewRoutes registers every handler for its Kinds. Later plan items add
// handlers without changing this signature.
func NewRoutes(handlers ...RoutedHandler) (*Router, error) {
	return nil, notimpl.Err("2.3 routes")
}
