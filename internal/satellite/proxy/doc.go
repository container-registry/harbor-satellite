// Package proxy provides Satellite's HTTP boundary for OCI Distribution
// requests.
//
// A HandlerFunc handles one parsed request, while a MiddlewareFunc decorates it:
//
//	type HandlerFunc func(*Request) error
//	type MiddlewareFunc func(HandlerFunc) HandlerFunc
//
// New accepts the terminal handler. Wrap immediately decorates the current handler,
// WrapAll applies several wrappers in argument order, and Handler adapts the
// composed handler to http.Handler:
//
//	handler := proxy.New(forwarder).
//		Wrap(authenticate).
//		Wrap(logger).
//		Handler()
//
// This composition executes logger(authenticate(forwarder)): the last wrapper
// applied is the outermost. A wrapper may run work before and after the supplied
// HandlerFunc, return an error without calling it, or satisfy the request directly.
// Continuation is an ordinary function call; Request does not contain orchestration
// state.
//
// Handler captures the composed handler and creates one Request for every HTTP
// exchange. It validates and classifies the request before application handling
// starts. A nil terminal handler leaves http.DefaultServeMux as the resulting
// handler, and nil wrappers are ignored.
//
// Request retains the original *http.Request, normalized OCI endpoint fields,
// and a private response writer. HTTPRequest exposes the original request without
// cloning it. ResponseHeader, Write, WriteResponse, and WriteError are the response
// boundary. WriteResponse streams an upstream body without buffering the complete
// response. An error returned after output starts does not append another response.
//
// Paths are classified by scanning fixed OCI endpoint suffixes from right to left,
// preserving repository names of arbitrary valid depth. Repository names, tags,
// digests, upload identifiers, relevant query parameters, and methods are validated
// before the handler chain runs. The package recognizes the OCI Distribution v1.1.1
// endpoint set and excludes extensions such as the catalog endpoint.
//
// Handler errors returned before response output are serialized with standard OCI
// Distribution v1.1.1 error codes. Unexpected errors receive a plain HTTP 500
// response. Configure a proxy before serving it; shared state captured by a
// HandlerFunc or MiddlewareFunc remains responsible for its own concurrency safety.
package proxy
