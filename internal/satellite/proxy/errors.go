package proxy

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrorCode is an error identifier defined by OCI Distribution Specification
// v1.1.1. Values outside this set must not be sent on the wire.
// Refer: https://specs.opencontainers.org/distribution-spec/?v=v1.1.1#DISTRIBUTION-SPEC-190
type ErrorCode string

const (
	ErrorCodeBlobUnknown         ErrorCode = "BLOB_UNKNOWN"
	ErrorCodeBlobUploadInvalid   ErrorCode = "BLOB_UPLOAD_INVALID"
	ErrorCodeBlobUploadUnknown   ErrorCode = "BLOB_UPLOAD_UNKNOWN"
	ErrorCodeDigestInvalid       ErrorCode = "DIGEST_INVALID"
	ErrorCodeManifestBlobUnknown ErrorCode = "MANIFEST_BLOB_UNKNOWN"
	ErrorCodeManifestInvalid     ErrorCode = "MANIFEST_INVALID"
	ErrorCodeManifestUnknown     ErrorCode = "MANIFEST_UNKNOWN"
	ErrorCodeNameInvalid         ErrorCode = "NAME_INVALID"
	ErrorCodeNameUnknown         ErrorCode = "NAME_UNKNOWN"
	ErrorCodeSizeInvalid         ErrorCode = "SIZE_INVALID"
	ErrorCodeUnauthorized        ErrorCode = "UNAUTHORIZED"
	ErrorCodeDenied              ErrorCode = "DENIED"
	ErrorCodeUnsupported         ErrorCode = "UNSUPPORTED"
	ErrorCodeTooManyRequests     ErrorCode = "TOOMANYREQUESTS"
)

// DistributionError is one entry in an OCI Distribution error envelope.
// HTTP status belongs to this occurrence because one OCI code may be returned
// with different statuses, such as UNSUPPORTED for a 404 route or 405 method.
// Refer: https://specs.opencontainers.org/distribution-spec/?v=v1.1.1#error-codes
type DistributionError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message,omitempty"`
	Detail  any       `json:"detail,omitempty"`

	httpStatus   int
	allowHeaders []string
}

// NewError returns an OCI Distribution error with the code's conventional HTTP
// status. Occurrence-specific status overrides are internal to the adapter.
func NewError(code ErrorCode, message string, detail any) error {
	if !code.Valid() {
		return fmt.Errorf("proxy: invalid OCI Distribution error code %q", code)
	}
	return newDistributionError(code.HTTPStatus(), code, message, detail)
}

func newDistributionError(
	httpStatus int,
	code ErrorCode,
	message string,
	detail any,
) *DistributionError {
	if message == "" {
		message = code.Message()
	}
	return &DistributionError{
		Code:       code,
		Message:    message,
		Detail:     detail,
		httpStatus: httpStatus,
	}
}

// Error implements error.
func (distributionError *DistributionError) Error() string {
	return distributionError.Message
}

// HTTPStatus returns the HTTP status selected for this error occurrence.
func (distributionError *DistributionError) HTTPStatus() int {
	if distributionError.httpStatus != 0 {
		return distributionError.httpStatus
	}
	return distributionError.Code.HTTPStatus()
}

// Valid reports whether the code is defined by OCI Distribution Specification
// v1.1.1.
func (code ErrorCode) Valid() bool {
	switch code {
	case ErrorCodeBlobUnknown,
		ErrorCodeBlobUploadInvalid,
		ErrorCodeBlobUploadUnknown,
		ErrorCodeDigestInvalid,
		ErrorCodeManifestBlobUnknown,
		ErrorCodeManifestInvalid,
		ErrorCodeManifestUnknown,
		ErrorCodeNameInvalid,
		ErrorCodeNameUnknown,
		ErrorCodeSizeInvalid,
		ErrorCodeUnauthorized,
		ErrorCodeDenied,
		ErrorCodeUnsupported,
		ErrorCodeTooManyRequests:
		return true
	default:
		return false
	}
}

// Message returns the specification description for the code.
func (code ErrorCode) Message() string {
	switch code {
	case ErrorCodeBlobUnknown:
		return "blob unknown to registry"
	case ErrorCodeBlobUploadInvalid:
		return "blob upload invalid"
	case ErrorCodeBlobUploadUnknown:
		return "blob upload unknown to registry"
	case ErrorCodeDigestInvalid:
		return "provided digest did not match uploaded content"
	case ErrorCodeManifestBlobUnknown:
		return "manifest references a manifest or blob unknown to registry"
	case ErrorCodeManifestInvalid:
		return "manifest invalid"
	case ErrorCodeManifestUnknown:
		return "manifest unknown to registry"
	case ErrorCodeNameInvalid:
		return "invalid repository name"
	case ErrorCodeNameUnknown:
		return "repository name not known to registry"
	case ErrorCodeSizeInvalid:
		return "provided length did not match content length"
	case ErrorCodeUnauthorized:
		return "authentication required"
	case ErrorCodeDenied:
		return "requested access to the resource is denied"
	case ErrorCodeUnsupported:
		return "the operation is unsupported"
	case ErrorCodeTooManyRequests:
		return "too many requests"
	default:
		return ErrorCodeUnsupported.Message()
	}
}

// HTTPStatus returns the conventional HTTP status for an OCI error code.
func (code ErrorCode) HTTPStatus() int {
	switch code {
	case ErrorCodeBlobUnknown,
		ErrorCodeBlobUploadUnknown,
		ErrorCodeManifestUnknown,
		ErrorCodeNameUnknown:
		return http.StatusNotFound
	case ErrorCodeUnauthorized:
		return http.StatusUnauthorized
	case ErrorCodeDenied:
		return http.StatusForbidden
	case ErrorCodeUnsupported:
		return http.StatusMethodNotAllowed
	case ErrorCodeTooManyRequests:
		return http.StatusTooManyRequests
	case ErrorCodeBlobUploadInvalid,
		ErrorCodeDigestInvalid,
		ErrorCodeManifestBlobUnknown,
		ErrorCodeManifestInvalid,
		ErrorCodeNameInvalid,
		ErrorCodeSizeInvalid:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func newErrorRouteNotFound(path string) error {
	return newDistributionError(
		http.StatusNotFound,
		ErrorCodeUnsupported,
		"unsupported OCI distribution route",
		map[string]string{"path": path},
	)
}

func newErrorMethodNotAllowed(method Method, allowed ...Method) error {
	distributionError := newDistributionError(
		http.StatusMethodNotAllowed,
		ErrorCodeUnsupported,
		"HTTP method is not supported for this OCI distribution route",
		map[string]string{"method": method.String()},
	)
	distributionError.allowHeaders = make([]string, len(allowed))
	for index, allowedMethod := range allowed {
		distributionError.allowHeaders[index] = allowedMethod.String()
	}
	return distributionError
}

func newErrorMalformedQuery(endpoint endpointDescriptor, method Method) error {
	if endpoint.keywords.blobs && endpoint.keywords.uploads {
		if endpoint.reference != "" && method == PUT {
			return NewError(
				ErrorCodeDigestInvalid,
				"one valid digest query parameter is required",
				nil,
			)
		}
		return NewError(ErrorCodeBlobUploadInvalid, "invalid upload query", nil)
	}
	return newErrorInvalidQuery("invalid endpoint query")
}

func newErrorInvalidQuery(message string) error {
	return newDistributionError(
		http.StatusBadRequest,
		ErrorCodeUnsupported,
		message,
		nil,
	)
}

func newErrorInvalidPath(path, message string) error {
	code := ErrorCodeUnsupported
	if strings.HasPrefix(path, apiPrefix) {
		code = ErrorCodeNameInvalid
	}

	return newDistributionError(http.StatusBadRequest, code, message, nil)
}

func newErrorInvalidHTTPRequest() error {
	return errors.New("proxy: HTTP request and URL are required")
}
