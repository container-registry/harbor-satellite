package async

import (
	"context"
	"errors"
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
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
type Router struct {
	routes map[fleet.EventKind][]Handler
}

func NewRouter() *Router { return &Router{routes: map[fleet.EventKind][]Handler{}} }

func (r *Router) Register(kind fleet.EventKind, h Handler) error {
	if h == nil {
		return fmt.Errorf("%w: nil handler for %s", fleet.ErrInvalid, kind)
	}
	if r.routes == nil {
		r.routes = map[fleet.EventKind][]Handler{}
	}
	r.routes[kind] = append(r.routes[kind], h)
	return nil
}

// Handle runs every handler of the event's kind; all of them run even
// when one fails, because each converges on its own.
func (r *Router) Handle(ctx context.Context, event OutboxEvent) error {
	handlers := r.routes[event.Kind]
	if len(handlers) == 0 {
		return fmt.Errorf("%s: %w", event.Kind, ErrNoHandler)
	}
	var errs []error
	for _, h := range handlers {
		if err := h.Handle(ctx, event); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// NewRoutes registers every handler for its Kinds. Later plan items add
// handlers without changing this signature.
func NewRoutes(handlers ...RoutedHandler) (*Router, error) {
	r := NewRouter()
	for i, h := range handlers {
		if h == nil {
			return nil, fmt.Errorf("%w: routed handler %d is nil", fleet.ErrInvalid, i)
		}
		for _, kind := range h.Kinds() {
			if err := r.Register(kind, h); err != nil {
				return nil, err
			}
		}
	}
	return r, nil
}
