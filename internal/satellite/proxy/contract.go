package proxy

import (
	"context"
	"net/http"

	digest "github.com/opencontainers/go-digest"
)

// Contract is the mutable, strongly typed state shared by one HTTP request
// pipeline. Request and Response belong to this request and are not cloned.
type Contract struct {
	Request  *http.Request
	Response http.ResponseWriter

	Operation        Operation
	Access           Access
	Repository       string
	Reference        string
	Digest           digest.Digest
	UploadID         string
	SourceRepository string
}

// Context returns the request cancellation and deadline context.
func (contract *Contract) Context() context.Context {
	return contract.Request.Context()
}

// newContract is the mandatory HTTP-to-domain parser used by the adapter.
func newContract(
	response http.ResponseWriter,
	request *http.Request,
) (*Contract, error) {
	if request == nil || request.URL == nil {
		return nil, errInvalidHTTPRequest
	}

	requestPath, err := canonicalPath(request.URL)
	if err != nil {
		return nil, err
	}
	contract := &Contract{
		Request:          request,
		Response:         response,
		Operation:        UnknownOperation,
		Access:           UnknownAccess,
		Repository:       "",
		Reference:        "",
		Digest:           "",
		UploadID:         "",
		SourceRepository: "",
	}
	if err := parseEndpoint(requestPath, contract); err != nil {
		return nil, err
	}
	return contract, nil
}
