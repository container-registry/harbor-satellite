package proxy

import "net/http"

// proxy holds the currently composed process until Handler builds the HTTP
// adapter.
type proxy struct {
	process Process
}

// New returns a proxy configuration for a terminal process. A nil process leaves
// http.DefaultServeMux as the final HTTP handler.
func New(process Process) *proxy {
	return &proxy{process: process}
}

// Wrap immediately wraps the current process with processor. A nil processor is
// ignored. Successive calls make the last processor the outermost one.
func (p *proxy) Wrap(processor Processor) *proxy {
	if processor != nil {
		p.process = processor(p.process)
	}
	return p
}

// WrapAll immediately wraps the current process with each processor in argument
// order. Nil processors are ignored, and the last processor becomes outermost.
func (p *proxy) WrapAll(processors ...Processor) *proxy {
	for i := len(processors) - 1; i >= 0; i-- {
		processor := processors[i]
		if processor != nil {
			p.process = processor(p.process)
		}
	}
	return p
}

// Handler adapts the composed process to http.Handler.
func (p *proxy) Handler() http.Handler {
	if p.process == nil {
		return http.DefaultServeMux
	}
	process := p.process

	return http.HandlerFunc(func(response http.ResponseWriter, httpRequest *http.Request) {
		request, err := newRequest(response, httpRequest)
		if err != nil {
			if writeErr := writeError(response, httpRequest, err); writeErr != nil {
				return
			}
			return
		}
		if err := process(request); err != nil && !request.responseWritten() {
			if writeErr := request.WriteError(err); writeErr != nil {
				return
			}
		}
	})
}
