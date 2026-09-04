package proxy

import "net/http"

// proxy holds the currently composed handler until Handler builds the HTTP
// adapter.
type proxy struct {
	handler HandlerFunc
}

// New returns a proxy configuration for a terminal handler. A nil handler leaves
// http.DefaultServeMux as the final HTTP handler.
func New(handler HandlerFunc) *proxy {
	return &proxy{handler: handler}
}

// Wrap immediately wraps the current handler with wrapper. A nil wrapper is
// ignored. Successive calls make the last wrapper the outermost one.
func (p *proxy) Wrap(wrapper MiddlewareFunc) *proxy {
	if wrapper != nil {
		p.handler = wrapper(p.handler)
	}
	return p
}

// WrapAll immediately wraps the current handler with each wrapper in argument
// order. Nil wrappers are ignored, and the last wrapper becomes outermost.
func (p *proxy) WrapAll(wrappers ...MiddlewareFunc) *proxy {
	for _, wrapper := range wrappers {
		if wrapper != nil {
			p.handler = wrapper(p.handler)
		}
	}
	return p
}

// Handler adapts the composed HandlerFunc to http.Handler.
func (p *proxy) Handler() http.Handler {
	if p.handler == nil {
		return http.DefaultServeMux
	}
	handler := p.handler

	return http.HandlerFunc(func(response http.ResponseWriter, httpRequest *http.Request) {
		request, err := newRequest(response, httpRequest)
		if err != nil {
			if writeErr := writeError(response, httpRequest, err); writeErr != nil {
				return
			}
			return
		}
		if err := handler(request); err != nil && !request.responseWritten() {
			if writeErr := request.WriteError(err); writeErr != nil {
				return
			}
		}
	})
}
