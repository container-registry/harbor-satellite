package proxy

import (
	"fmt"
	"net/http"
)

// Stack holds a handler chain composed once during construction.
type Stack struct {
	handler Handler
}

// New composes layers in request execution order. A stack does not require a
// base handler; zero layers form a valid identity chain.
func New(layers ...Layer) (*Stack, error) {
	handler := Handler(HandlerFunc(func(*Contract) error { return nil }))
	for index := len(layers) - 1; index >= 0; index-- {
		if layers[index] == nil {
			return nil, fmt.Errorf("proxy: layer %d is nil", index)
		}
		handler = layers[index].Wrap(handler)
		if handler == nil {
			return nil, fmt.Errorf("proxy: layer %d returned a nil handler", index)
		}
	}
	return &Stack{handler: handler}, nil
}

// Handler exposes the composed chain as an HTTP handler.
func (stack *Stack) Handler() http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		contract, err := newContract(response, request)
		if err != nil {
			handleError(response, request, err)
			return
		}
		if err := stack.handler.Handle(contract); err != nil {
			handleError(response, request, err)
		}
	})
}
