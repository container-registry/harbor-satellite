package proxy

import "strings"

// Query contains the validated query parameters defined by OCI Distribution
// endpoints. Pointers distinguish an absent parameter from a valid zero or empty
// primitive value; endpoint validation rejects invalid empty values.
type Query struct {
	N            *int
	Last         *string
	Mount        *string
	From         *string
	ArtifactType *string
	Digest       *string
}

// endpointKeywords records the static words found at the end of an OCI path.
// Combinations distinguish endpoint families without depending on repository depth.
type endpointKeywords struct {
	ping      bool
	manifests bool
	blobs     bool
	uploads   bool
	tags      bool
	list      bool
	referrers bool
}

func (keywords endpointKeywords) known() bool {
	return keywords.ping || keywords.manifests || keywords.blobs ||
		keywords.tags || keywords.referrers
}

// endpointDescriptor is the result of one reverse scan of the path. Reference
// is a manifest reference, digest, or upload identifier.
type endpointDescriptor struct {
	keywords   endpointKeywords
	repository string
	reference  string
}

// describeEndpoint scans from the fixed right-hand side of the path. Repository
// components are never split or counted, so arbitrary valid repository depth is
// retained as one substring.
func describeEndpoint(requestPath string) endpointDescriptor {
	endpoint := endpointDescriptor{}
	if requestPath == "/v2" || requestPath == "/v2/" {
		endpoint.keywords.ping = true
		return endpoint
	}
	if !strings.HasPrefix(requestPath, apiPrefix) {
		return endpoint
	}

	remainder := requestPath[len(apiPrefix):]
	parent, last, found := cutLastSegment(remainder)
	if !found {
		return endpoint
	}

	switch last {
	case "":
		// /v2/<name>/blobs/uploads/: end-4a, end-4b, end-11
		return describeUploadStart(parent, endpoint)
	case "list":
		// /v2/<name>/tags/list: end-8a, end-8b
		repository, keyword, ok := cutLastSegment(parent)
		if ok && keyword == "tags" {
			endpoint.keywords.tags = true
			endpoint.keywords.list = true
			endpoint.repository = repository
		}
		return endpoint
	default:
		return describeValueEndpoint(parent, last, endpoint)
	}
}

func describeUploadStart(parent string, endpoint endpointDescriptor) endpointDescriptor {
	repositoryAndBlobs, keyword, found := cutLastSegment(parent)
	if !found || keyword != "uploads" {
		return endpoint
	}
	repository, keyword, found := cutLastSegment(repositoryAndBlobs)
	if !found || keyword != "blobs" {
		return endpoint
	}
	endpoint.keywords.blobs = true
	endpoint.keywords.uploads = true
	endpoint.repository = repository
	return endpoint
}

func describeValueEndpoint(
	parent string,
	reference string,
	endpoint endpointDescriptor,
) endpointDescriptor {
	repository, keyword, found := cutLastSegment(parent)
	if !found {
		return endpoint
	}
	switch keyword {
	case "manifests":
		endpoint.keywords.manifests = true
		endpoint.repository = repository
		endpoint.reference = reference
	case "blobs":
		endpoint.keywords.blobs = true
		endpoint.repository = repository
		endpoint.reference = reference
	case "referrers":
		endpoint.keywords.referrers = true
		endpoint.repository = repository
		endpoint.reference = reference
	case "uploads":
		repository, keyword, found = cutLastSegment(repository)
		if found && keyword == "blobs" {
			endpoint.keywords.blobs = true
			endpoint.keywords.uploads = true
			endpoint.repository = repository
			endpoint.reference = reference
		}
	}
	return endpoint
}

func cutLastSegment(value string) (parent, last string, found bool) {
	index := strings.LastIndexByte(value, '/')
	if index < 0 {
		return "", "", false
	}
	return value[:index], value[index+1:], true
}
