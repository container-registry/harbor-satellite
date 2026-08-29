package proxy

import (
	"net/url"

	digest "github.com/opencontainers/go-digest"
)

const apiPrefix = "/v2/"

// parseEndpoint classifies and validates only endpoints defined by OCI
// Distribution Specification v1.1.1. The path is described once, after which
// this switch makes the accepted endpoint surface explicit.
//
// Refer: https://specs.opencontainers.org/distribution-spec/?v=v1.1.1#endpoints
func parseEndpoint(requestPath string, request *Request) error {
	endpoint := describeEndpoint(requestPath)
	if !endpoint.keywords.known() {
		return newErrorRouteNotFound(requestPath)
	}

	// end-1: GET /v2/.
	if endpoint.keywords.ping {
		if request.Method != GET {
			return newErrorMethodNotAllowed(request.Method, GET)
		}
		request.Operation = CheckRegistry
		return nil
	}

	if err := setRepository(request, endpoint.repository); err != nil {
		return err
	}
	query, err := url.ParseQuery(request.httpRequest.URL.RawQuery)
	if err != nil {
		return newErrorMalformedQuery(endpoint, request.Method)
	}
	if err := populateQuery(endpoint, query, request); err != nil {
		return err
	}

	switch {
	// end-3, end-7, end-9: read, publish, or delete a manifest.
	case endpoint.keywords.manifests:
		return populateManifest(endpoint, request)

	case endpoint.keywords.blobs:
		switch {
		// end-4a, end-4b, end-11: start or perform an upload, or mount a blob.
		case endpoint.keywords.uploads && endpoint.reference == "":
			return populateBlobUploadStart(request)

		// end-5, end-6, end-13: append, complete, or inspect an upload.
		case endpoint.keywords.uploads:
			return populateBlobUploadSession(endpoint, request)

		// end-2, end-10: read or delete a blob.
		default:
			return populateBlob(endpoint, request)
		}

	// end-8a, end-8b: list tags, optionally with pagination.
	case endpoint.keywords.tags && endpoint.keywords.list:
		return populateTags(request)

	// end-12a, end-12b: list referrers, optionally filtered by artifact type.
	case endpoint.keywords.referrers:
		return populateReferrers(endpoint, request)

	default:
		return newErrorRouteNotFound(requestPath)
	}
}

// populateQuery validates and normalizes the query parameters belonging to the
// classified endpoint before operation-specific request population begins.
func populateQuery(endpoint endpointDescriptor, values url.Values, request *Request) error {
	switch {
	// end-4a, end-4b, end-11: upload start, monolithic upload, or mount.
	case endpoint.keywords.blobs && endpoint.keywords.uploads &&
		endpoint.reference == "" && request.Method == POST:
		hasMount := values.Has("mount")
		hasFrom := values.Has("from")
		hasDigest := values.Has("digest")
		if hasMount || hasFrom {
			if !hasMount || !hasFrom || hasDigest ||
				len(values["mount"]) != 1 || len(values["from"]) != 1 {
				return NewError(
					ErrorCodeBlobUploadInvalid,
					"mount and from must each be supplied once",
					nil,
				)
			}

			mount, err := digest.Parse(values.Get("mount"))
			if err != nil {
				return NewError(ErrorCodeDigestInvalid, "invalid mount digest", nil)
			}
			from := values.Get("from")
			if !validRepository(request.httpRequest.Host, from) {
				return NewError(
					ErrorCodeNameInvalid,
					"invalid mount source repository",
					nil,
				)
			}

			mountValue := mount.String()
			request.Query.Mount = &mountValue
			request.Query.From = &from
			return nil
		}

		if !hasDigest {
			return nil
		}
		if len(values["digest"]) != 1 {
			return NewError(ErrorCodeDigestInvalid, "invalid upload digest", nil)
		}
		uploadDigest, err := digest.Parse(values.Get("digest"))
		if err != nil {
			return NewError(ErrorCodeDigestInvalid, "invalid upload digest", nil)
		}
		digestValue := uploadDigest.String()
		request.Query.Digest = &digestValue
		return nil

	// end-6: upload completion requires exactly one digest.
	case endpoint.keywords.blobs && endpoint.keywords.uploads &&
		endpoint.reference != "" && request.Method == PUT:
		if len(values["digest"]) != 1 {
			return NewError(
				ErrorCodeDigestInvalid,
				"one valid digest query parameter is required",
				nil,
			)
		}
		uploadDigest, err := digest.Parse(values.Get("digest"))
		if err != nil {
			return NewError(
				ErrorCodeDigestInvalid,
				"one valid digest query parameter is required",
				nil,
			)
		}
		digestValue := uploadDigest.String()
		request.Query.Digest = &digestValue
		return nil

	// end-8a, end-8b: optional tag pagination.
	case endpoint.keywords.tags && endpoint.keywords.list && request.Method == GET:
		if values.Has("n") {
			limit, valid := parseTagLimit(values.Get("n"))
			if !valid {
				return newErrorInvalidQuery("n must be a non-negative integer")
			}
			request.Query.N = &limit
		}
		if values.Has("last") {
			last := values.Get("last")
			if !validTag(last) {
				return newErrorInvalidQuery("last must be one valid tag")
			}
			request.Query.Last = &last
		}
		return nil

	// end-12b: optional referrer artifact type filtering.
	case endpoint.keywords.referrers && request.Method == GET:
		if !values.Has("artifactType") {
			return nil
		}
		artifactType, valid := parseArtifactType(values.Get("artifactType"))
		if !valid {
			return newErrorInvalidQuery("artifactType must be a valid media type")
		}
		request.Query.ArtifactType = &artifactType
		return nil

	default:
		return nil
	}
}

// populateManifest handles manifest endpoints end-3, end-7, and end-9.
func populateManifest(endpoint endpointDescriptor, request *Request) error {
	request.Reference = endpoint.reference
	if parsed, err := digest.Parse(endpoint.reference); err == nil {
		request.Digest = parsed
	} else if !validTag(endpoint.reference) {
		return NewError(ErrorCodeManifestInvalid, "invalid manifest reference", nil)
	}

	switch request.Method {
	// end-3: pull a manifest or check whether it exists.
	case GET:
		request.Operation = PullManifest
	case HEAD:
		request.Operation = CheckManifest
	// end-7: push a manifest.
	case PUT:
		request.Operation = PushManifest
	// end-9: delete a manifest.
	case DELETE:
		request.Operation = DeleteManifest
	case Unknown, POST, PATCH:
		return newErrorMethodNotAllowed(
			request.Method,
			GET,
			HEAD,
			PUT,
			DELETE,
		)
	default:
		return newErrorMethodNotAllowed(request.Method, GET, HEAD, PUT, DELETE)
	}
	return nil
}

// populateBlob handles blob endpoints end-2 and end-10.
func populateBlob(endpoint endpointDescriptor, request *Request) error {
	parsed, err := digest.Parse(endpoint.reference)
	if err != nil {
		return NewError(ErrorCodeDigestInvalid, "invalid blob digest", nil)
	}
	request.Digest = parsed

	switch request.Method {
	// end-2: pull a blob or check whether it exists.
	case GET:
		request.Operation = PullBlob
	case HEAD:
		request.Operation = CheckBlob
	// end-10: delete a blob.
	case DELETE:
		request.Operation = DeleteBlob
	case Unknown, POST, PATCH, PUT:
		return newErrorMethodNotAllowed(
			request.Method,
			GET,
			HEAD,
			DELETE,
		)
	default:
		return newErrorMethodNotAllowed(request.Method, GET, HEAD, DELETE)
	}
	return nil
}

// populateBlobUploadStart handles upload endpoints end-4a, end-4b, and end-11.
func populateBlobUploadStart(request *Request) error {
	// All three endpoint forms use POST and differ through their query parameters.
	if request.Method != POST {
		return newErrorMethodNotAllowed(request.Method, POST)
	}
	request.Operation = StartBlobUpload
	if request.Query.Mount != nil {
		// end-11: mount a blob from another repository.
		request.Operation = MountBlob
		request.Digest = digest.Digest(*request.Query.Mount)
		request.SourceRepository = *request.Query.From
		return nil
	}

	if request.Query.Digest == nil {
		// end-4a: start a resumable upload session.
		return nil
	}
	// end-4b: perform a monolithic upload with the expected digest.
	request.Operation = PushBlob
	request.Digest = digest.Digest(*request.Query.Digest)
	return nil
}

// populateBlobUploadSession handles upload endpoints end-5, end-6, and end-13.
func populateBlobUploadSession(endpoint endpointDescriptor, request *Request) error {
	if !validOpaqueSegment(endpoint.reference) {
		return NewError(ErrorCodeBlobUploadInvalid, "invalid upload ID", nil)
	}
	request.UploadID = endpoint.reference

	switch request.Method {
	// end-13: inspect the current upload offset and location.
	case GET:
		request.Operation = CheckBlobUpload
	// end-5: append a chunk to an active upload.
	case PATCH:
		request.Operation = UpdateBlobUpload
	// end-6: complete an upload using the expected digest.
	case PUT:
		request.Operation = CompleteBlobUpload
		if request.Query.Digest == nil {
			return NewError(
				ErrorCodeDigestInvalid,
				"one valid digest query parameter is required",
				nil,
			)
		}
		request.Digest = digest.Digest(*request.Query.Digest)
	case Unknown, POST, DELETE, HEAD:
		return newErrorMethodNotAllowed(
			request.Method,
			GET,
			PATCH,
			PUT,
		)
	default:
		return newErrorMethodNotAllowed(request.Method, GET, PATCH, PUT)
	}
	return nil
}

// populateTags handles tag-listing endpoints end-8a and end-8b.
func populateTags(request *Request) error {
	// end-8a and end-8b: list tags, with optional pagination parameters.
	if request.Method != GET {
		return newErrorMethodNotAllowed(request.Method, GET)
	}
	request.Operation = ListTags
	return nil
}

// populateReferrers handles referrer endpoints end-12a and end-12b.
func populateReferrers(endpoint endpointDescriptor, request *Request) error {
	// end-12a and end-12b: list referrers, optionally filtered by artifactType.
	if request.Method != GET {
		return newErrorMethodNotAllowed(request.Method, GET)
	}
	request.Operation = ListReferrers
	parsed, err := digest.Parse(endpoint.reference)
	if err != nil {
		return NewError(ErrorCodeDigestInvalid, "invalid subject digest", nil)
	}
	request.Digest = parsed
	return nil
}

func setRepository(request *Request, repository string) error {
	if !validRepository(request.httpRequest.Host, repository) {
		return NewError(ErrorCodeNameInvalid, "invalid repository name", nil)
	}
	request.Repository = repository
	return nil
}
