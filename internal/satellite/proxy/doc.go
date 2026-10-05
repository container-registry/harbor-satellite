// Package proxy provides Satellite's HTTP boundary for OCI Distribution
// requests.
//
// A Process handles one parsed request, while a Processor decorates a Process:
//
//	type Process func(*Request) error
//	type Processor func(Process) Process
//
// New accepts the terminal process. Wrap immediately decorates the current process,
// WrapAll applies several processors in argument order, and Handler adapts the
// composed process to http.Handler:
//
//	handler := proxy.New(forwarder).
//		Wrap(authenticate).
//		Wrap(logger).
//		Handler()
//
// This composition executes logger(authenticate(forwarder)): the last processor
// wrapped is the outermost. A processor may run work before and after the supplied
// Process, return an error without calling it, or satisfy the request directly.
// Continuation is an ordinary function call; Request does not contain orchestration
// state.
//
// Handler captures the composed process and creates one Request for every HTTP
// exchange. It validates and classifies the request before application processing
// starts. A nil terminal process leaves http.DefaultServeMux as the resulting
// handler, and nil processors are ignored.
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
// before the process chain runs. The package recognizes the OCI Distribution v1.1.1
// endpoint set and excludes extensions such as the catalog endpoint.
//
// Process errors returned before response output are serialized with standard OCI
// Distribution v1.1.1 error codes. Unexpected errors receive a plain HTTP 500
// response. Configure a proxy before serving it; shared state captured by a Process
// or Processor remains responsible for its own concurrency safety.
package proxy
