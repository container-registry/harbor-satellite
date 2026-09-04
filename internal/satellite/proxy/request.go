package proxy

import (
	"context"
	"net/http"

	digest "github.com/opencontainers/go-digest"
)

// HandlerFunc handles one validated OCI Distribution request.
type HandlerFunc func(*Request) error

// MiddlewareFunc decorates a HandlerFunc and controls whether and when it continues.
type MiddlewareFunc func(HandlerFunc) HandlerFunc

// httpExchange owns the transport objects and response state for one request.
// It is embedded so transport ownership remains private to the parsed request.
type httpExchange struct {
	httpRequest    *http.Request
	responseWriter http.ResponseWriter
	written        bool
}

// Request is the mutable, strongly typed state shared while processing one
// validated OCI Distribution request.
type Request struct {
	httpExchange

	Operation        Operation
	Method           Method
	Query            Query
	Repository       string
	Reference        string
	Digest           digest.Digest
	UploadID         string
	SourceRepository string
}

// Resource returns the coarse resource class derived from the operation.
func (request *Request) Resource() Resource {
	return request.Operation.Resource()
}

// HTTPRequest returns the original HTTP request. The pointer is not cloned.
func (request *Request) HTTPRequest() *http.Request {
	return request.httpRequest
}

// Context returns the HTTP request cancellation and deadline context.
func (request *Request) Context() context.Context {
	return request.httpRequest.Context()
}

// newRequest is the mandatory HTTP-to-domain parser used by the adapter.
func newRequest(
	response http.ResponseWriter,
	httpRequest *http.Request,
) (*Request, error) {
	if httpRequest == nil || httpRequest.URL == nil {
		return nil, newErrorInvalidHTTPRequest()
	}

	requestPath, err := canonicalPath(httpRequest.URL)
	if err != nil {
		return nil, err
	}
	request := &Request{
		httpExchange: httpExchange{
			httpRequest:    httpRequest,
			responseWriter: response,
		},
		Operation: OperationUnknown,
		Method:    Method(httpRequest.Method),
	}
	if err := parseEndpoint(requestPath, request); err != nil {
		return nil, err
	}
	return request, nil
}
