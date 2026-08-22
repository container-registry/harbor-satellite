package proxy

import (
	"net/http"
	"strings"

	digest "github.com/opencontainers/go-digest"
)

const apiPrefix = "/v2/"

// parseEndpoint classifies and validates only endpoints defined by OCI
// Distribution Specification v1.1.1. The path is described once, after which
// this switch makes the accepted endpoint surface explicit.
//
// Refer: https://specs.opencontainers.org/distribution-spec/?v=v1.1.1#endpoints
func parseEndpoint(requestPath string, contract *Contract) error {
	endpoint := describeEndpoint(
		requestPath,
		contract.Request.URL.RawQuery,
	)
	if !endpoint.keywords.known() {
		return routeNotFound(requestPath)
	}

	// end-1: GET /v2/.
	if endpoint.keywords.ping {
		if contract.Request.Method != http.MethodGet {
			return methodNotAllowed(contract.Request.Method)
		}
		contract.Operation = Ping
		contract.Access = ReadAccess
		return nil
	}

	if err := setRepository(contract, endpoint.repository); err != nil {
		return err
	}

	switch {
	// end-3, end-7, end-9: read, publish, or delete a manifest.
	case endpoint.keywords.manifests:
		return populateManifest(endpoint, contract)

	case endpoint.keywords.blobs:
		switch {
		// end-4a, end-4b, end-11: start or perform an upload, or mount a blob.
		case endpoint.keywords.uploads && endpoint.reference == "":
			return populateBlobUploadStart(endpoint, contract)

		// end-5, end-6, end-13: append, complete, or inspect an upload.
		case endpoint.keywords.uploads:
			return populateBlobUploadSession(endpoint, contract)

		// end-2, end-10: read or delete a blob.
		default:
			return populateBlob(endpoint, contract)
		}

	// end-8a, end-8b: list tags, optionally with pagination.
	case endpoint.keywords.tags && endpoint.keywords.list:
		return populateTags(contract)

	// end-12a, end-12b: list referrers, optionally filtered by artifact type.
	case endpoint.keywords.referrers:
		return populateReferrers(endpoint, contract)

	default:
		return routeNotFound(requestPath)
	}
}

// populateManifest handles manifest endpoints end-3, end-7, and end-9.
func populateManifest(endpoint endpointDescriptor, contract *Contract) error {
	contract.Operation = Manifest
	contract.Reference = endpoint.reference
	if parsed, err := digest.Parse(endpoint.reference); err == nil {
		contract.Digest = parsed
	} else if !validTag(endpoint.reference) {
		return NewError(ErrorCodeManifestInvalid, "invalid manifest reference", nil)
	}

	switch contract.Request.Method {
	// end-3: pull a manifest or check whether it exists.
	case http.MethodGet, http.MethodHead:
		contract.Access = ReadAccess
	// end-7: push a manifest.
	case http.MethodPut:
		contract.Access = WriteAccess
	// end-9: delete a manifest.
	case http.MethodDelete:
		contract.Access = DeleteAccess
	default:
		return methodNotAllowed(contract.Request.Method)
	}
	return nil
}

// populateBlob handles blob endpoints end-2 and end-10.
func populateBlob(endpoint endpointDescriptor, contract *Contract) error {
	parsed, err := digest.Parse(endpoint.reference)
	if err != nil {
		return NewError(ErrorCodeDigestInvalid, "invalid blob digest", nil)
	}
	contract.Operation = Blob
	contract.Digest = parsed

	switch contract.Request.Method {
	// end-2: pull a blob or check whether it exists.
	case http.MethodGet, http.MethodHead:
		contract.Access = ReadAccess
	// end-10: delete a blob.
	case http.MethodDelete:
		contract.Access = DeleteAccess
	default:
		return methodNotAllowed(contract.Request.Method)
	}
	return nil
}

// populateBlobUploadStart handles upload endpoints end-4a, end-4b, and end-11.
func populateBlobUploadStart(endpoint endpointDescriptor, contract *Contract) error {
	// All three endpoint forms use POST and differ through their query parameters.
	if contract.Request.Method != http.MethodPost {
		return methodNotAllowed(contract.Request.Method)
	}
	if endpoint.query.invalid {
		return NewError(ErrorCodeBlobUploadInvalid, "invalid upload query", nil)
	}

	hasMount := endpoint.query.mount != nil
	hasFrom := endpoint.query.from != nil
	hasDigest := endpoint.query.digest != nil
	if hasMount || hasFrom {
		// end-11: mount a blob from another repository.
		if !hasMount || !hasFrom || hasDigest ||
			len(endpoint.query.mount) != 1 || len(endpoint.query.from) != 1 {
			return NewError(
				ErrorCodeBlobUploadInvalid,
				"mount and from must each be supplied once",
				nil,
			)
		}
		parsed, err := digest.Parse(endpoint.query.mount[0])
		if err != nil {
			return NewError(ErrorCodeDigestInvalid, "invalid mount digest", nil)
		}
		if !validRepository(contract.Request.Host, endpoint.query.from[0]) {
			return NewError(
				ErrorCodeNameInvalid,
				"invalid mount source repository",
				nil,
			)
		}
		contract.Operation = BlobMount
		contract.Access = WriteAccess
		contract.Digest = parsed
		contract.SourceRepository = endpoint.query.from[0]
		return nil
	}

	contract.Operation = BlobUpload
	contract.Access = WriteAccess
	if !hasDigest {
		// end-4a: start a resumable upload session.
		return nil
	}
	// end-4b: perform a monolithic upload with the expected digest.
	if len(endpoint.query.digest) != 1 {
		return NewError(
			ErrorCodeDigestInvalid,
			"one valid digest query parameter is required",
			nil,
		)
	}
	parsed, err := digest.Parse(endpoint.query.digest[0])
	if err != nil {
		return NewError(ErrorCodeDigestInvalid, "invalid upload digest", nil)
	}
	contract.Digest = parsed
	return nil
}

// populateBlobUploadSession handles upload endpoints end-5, end-6, and end-13.
func populateBlobUploadSession(endpoint endpointDescriptor, contract *Contract) error {
	if !validOpaqueSegment(endpoint.reference) {
		return NewError(ErrorCodeBlobUploadInvalid, "invalid upload ID", nil)
	}
	contract.Operation = BlobUpload
	contract.UploadID = endpoint.reference

	switch contract.Request.Method {
	// end-13: inspect the current upload offset and location.
	case http.MethodGet:
		contract.Access = ReadAccess
	// end-5: append a chunk to an active upload.
	case http.MethodPatch:
		contract.Access = WriteAccess
	// end-6: complete an upload using the expected digest.
	case http.MethodPut:
		if endpoint.query.invalid || len(endpoint.query.digest) != 1 {
			return NewError(
				ErrorCodeDigestInvalid,
				"one valid digest query parameter is required",
				nil,
			)
		}
		parsed, err := digest.Parse(endpoint.query.digest[0])
		if err != nil {
			return NewError(
				ErrorCodeDigestInvalid,
				"one valid digest query parameter is required",
				nil,
			)
		}
		contract.Access = WriteAccess
		contract.Digest = parsed
	default:
		return methodNotAllowed(contract.Request.Method)
	}
	return nil
}

// populateTags handles tag-listing endpoints end-8a and end-8b.
func populateTags(contract *Contract) error {
	// end-8a and end-8b: list tags, with optional pagination parameters.
	if contract.Request.Method != http.MethodGet {
		return methodNotAllowed(contract.Request.Method)
	}
	contract.Operation = Tags
	contract.Access = ReadAccess
	return nil
}

// populateReferrers handles referrer endpoints end-12a and end-12b.
func populateReferrers(endpoint endpointDescriptor, contract *Contract) error {
	// end-12a and end-12b: list referrers, optionally filtered by artifactType.
	if contract.Request.Method != http.MethodGet {
		return methodNotAllowed(contract.Request.Method)
	}
	parsed, err := digest.Parse(endpoint.reference)
	if err != nil {
		return NewError(ErrorCodeDigestInvalid, "invalid subject digest", nil)
	}
	contract.Operation = Referrers
	contract.Access = ReadAccess
	contract.Digest = parsed
	return nil
}

func setRepository(contract *Contract, repository string) error {
	if !validRepository(contract.Request.Host, repository) {
		return NewError(ErrorCodeNameInvalid, "invalid repository name", nil)
	}
	contract.Repository = repository
	return nil
}

func routeNotFound(path string) error {
	return newDistributionError(
		http.StatusNotFound,
		ErrorCodeUnsupported,
		"unsupported OCI distribution route",
		map[string]string{"path": path},
	)
}

func methodNotAllowed(method string) error {
	return newDistributionError(
		http.StatusMethodNotAllowed,
		ErrorCodeUnsupported,
		"HTTP method is not supported for this OCI distribution route",
		map[string]string{"method": method},
	)
}

func invalidPath(path, message string) error {
	code := ErrorCodeUnsupported
	if strings.HasPrefix(path, apiPrefix) {
		code = ErrorCodeNameInvalid
	}
	return newDistributionError(http.StatusBadRequest, code, message, nil)
}
