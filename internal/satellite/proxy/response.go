package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var errResponseWritten = errors.New("proxy: response has already been written")

type distributionErrorEnvelope struct {
	Errors []*DistributionError `json:"errors"`
}

// ResponseHeader returns the response headers for this request. Header values
// may be changed until a response is written.
func (request *Request) ResponseHeader() http.Header {
	return request.responseWriter.Header()
}

// Write sends a status and payload to the client.
func (request *Request) Write(statusCode int, payload []byte) error {
	if request.responseWritten() {
		return errResponseWritten
	}
	if len(payload) > 0 {
		header := request.responseWriter.Header()
		if header.Get("Content-Type") == "" {
			header.Set("Content-Type", http.DetectContentType(payload))
		}
		header.Set("Content-Length", strconv.Itoa(len(payload)))
	}

	request.written = true
	request.responseWriter.WriteHeader(statusCode)
	if request.Method == HEAD || len(payload) == 0 {
		return nil
	}
	_, err := request.responseWriter.Write(payload)
	return err
}

// WriteResponse forwards an HTTP response, including headers, status, and body.
// The body is streamed directly with io.Copy rather than buffered in memory, then
// closed before this method returns.
func (request *Request) WriteResponse(response *http.Response) error {
	if response == nil {
		return errors.New("proxy: HTTP response is required")
	}
	if request.responseWritten() {
		return errResponseWritten
	}

	copyResponseHeaders(request.responseWriter.Header(), response.Header)

	request.written = true
	request.responseWriter.WriteHeader(response.StatusCode)
	if response.Body == nil {
		return nil
	}
	defer response.Body.Close()
	if request.Method == HEAD {
		return nil
	}
	_, err := io.Copy(request.responseWriter, response.Body)
	return err
}

// WriteContent sends a content response to the client. Seekable bodies are
// served with net/http's range and conditional-request handling; other bodies
// use WriteResponse's direct streaming path.
func (request *Request) WriteContent(response *http.Response) error {
	if response == nil {
		return errors.New("proxy: HTTP response is required")
	}
	if request.responseWritten() {
		return errResponseWritten
	}

	seeker, seekable := response.Body.(io.ReadSeeker)
	if response.StatusCode != http.StatusOK || !seekable {
		return request.WriteResponse(response)
	}

	copyResponseHeaders(request.responseWriter.Header(), response.Header)
	request.written = true
	defer response.Body.Close()
	http.ServeContent(
		request.responseWriter,
		request.httpRequest,
		request.Reference,
		time.Time{},
		seeker,
	)
	return nil
}

func copyResponseHeaders(destination, source http.Header) {
	hopByHop := map[string]struct{}{
		"Connection":          {},
		"Keep-Alive":          {},
		"Proxy-Authenticate":  {},
		"Proxy-Authorization": {},
		"Proxy-Connection":    {},
		"Te":                  {},
		"Trailer":             {},
		"Transfer-Encoding":   {},
		"Upgrade":             {},
	}
	for _, value := range source.Values("Connection") {
		for token := range strings.SplitSeq(value, ",") {
			token = http.CanonicalHeaderKey(strings.TrimSpace(token))
			if token != "" {
				hopByHop[token] = struct{}{}
			}
		}
	}
	for key, values := range source {
		if _, found := hopByHop[http.CanonicalHeaderKey(key)]; found {
			continue
		}
		destination[key] = append([]string(nil), values...)
	}
}

// WriteError sends an OCI Distribution error envelope. Non-distribution errors
// are represented as a plain HTTP 500 response.
func (request *Request) WriteError(err error) error {
	if request.responseWritten() {
		return errResponseWritten
	}
	request.written = true
	return writeError(request.responseWriter, request.httpRequest, err)
}

func (request *Request) responseWritten() bool {
	return request.written
}

func writeError(response http.ResponseWriter, request *http.Request, err error) error {
	var distributionError *DistributionError
	if !errors.As(err, &distributionError) ||
		distributionError == nil ||
		!distributionError.Code.Valid() {
		return writeInternalError(response, request)
	}

	payload, marshalErr := json.Marshal(distributionErrorEnvelope{
		Errors: []*DistributionError{distributionError},
	})
	if marshalErr != nil {
		withoutDetail := *distributionError
		withoutDetail.Detail = nil
		payload, marshalErr = json.Marshal(distributionErrorEnvelope{
			Errors: []*DistributionError{&withoutDetail},
		})
		if marshalErr != nil {
			return writeInternalError(response, request)
		}
	}

	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	if len(distributionError.allowHeaders) > 0 {
		response.Header().Set("Allow", strings.Join(distributionError.allowHeaders, ", "))
	}
	response.WriteHeader(distributionError.HTTPStatus())
	if request != nil && request.Method == http.MethodHead {
		return nil
	}
	_, writeErr := response.Write(payload)
	return writeErr
}

func writeInternalError(response http.ResponseWriter, request *http.Request) error {
	payload := []byte(http.StatusText(http.StatusInternalServerError) + "\n")
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	response.WriteHeader(http.StatusInternalServerError)
	if request != nil && request.Method == http.MethodHead {
		return nil
	}
	if _, err := response.Write(payload); err != nil {
		return fmt.Errorf("proxy: write internal error response: %w", err)
	}
	return nil
}
