package proxy

import "net/http"

// Operation identifies one method-specific OCI Distribution operation.
// Constants combine an action and resource so a Request cannot represent an
// invalid pair such as pushing tags or listing a blob.
type Operation uint8

const (
	OperationUnknown Operation = iota
	CheckRegistry
	PullManifest
	CheckManifest
	PushManifest
	DeleteManifest
	PullBlob
	CheckBlob
	PushBlob
	DeleteBlob
	StartBlobUpload
	CheckBlobUpload
	UpdateBlobUpload
	CompleteBlobUpload
	MountBlob
	ListTags
	ListReferrers
)

// Resource identifies the OCI Distribution resource addressed by an operation.
type Resource uint8

const (
	ResourceUnknown Resource = iota
	ResourceRegistry
	ResourceManifest
	ResourceBlob
	ResourceBlobUpload
	ResourceTags
	ResourceReferrers
)

// Method is an HTTP method accepted by the OCI Distribution endpoint parser.
type Method string

const (
	Unknown Method = ""
	GET     Method = http.MethodGet
	POST    Method = http.MethodPost
	PATCH   Method = http.MethodPatch
	PUT     Method = http.MethodPut
	DELETE  Method = http.MethodDelete
	HEAD    Method = http.MethodHead
)

// String returns the stable policy name of an operation.
func (operation Operation) String() string {
	switch operation {
	case CheckRegistry:
		return "check_registry"
	case PullManifest:
		return "pull_manifest"
	case CheckManifest:
		return "check_manifest"
	case PushManifest:
		return "push_manifest"
	case DeleteManifest:
		return "delete_manifest"
	case PullBlob:
		return "pull_blob"
	case CheckBlob:
		return "check_blob"
	case PushBlob:
		return "push_blob"
	case DeleteBlob:
		return "delete_blob"
	case StartBlobUpload:
		return "start_blob_upload"
	case CheckBlobUpload:
		return "check_blob_upload"
	case UpdateBlobUpload:
		return "update_blob_upload"
	case CompleteBlobUpload:
		return "complete_blob_upload"
	case MountBlob:
		return "mount_blob"
	case ListTags:
		return "list_tags"
	case ListReferrers:
		return "list_referrers"
	case OperationUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// Resource returns the coarse resource class addressed by an operation.
func (operation Operation) Resource() Resource {
	switch operation {
	case CheckRegistry:
		return ResourceRegistry
	case PullManifest, CheckManifest, PushManifest, DeleteManifest:
		return ResourceManifest
	case PullBlob, CheckBlob, PushBlob, DeleteBlob, MountBlob:
		return ResourceBlob
	case StartBlobUpload, CheckBlobUpload, UpdateBlobUpload, CompleteBlobUpload:
		return ResourceBlobUpload
	case ListTags:
		return ResourceTags
	case ListReferrers:
		return ResourceReferrers
	case OperationUnknown:
		return ResourceUnknown
	default:
		return ResourceUnknown
	}
}

// IsPull reports whether the operation returns manifest or blob content.
func (operation Operation) IsPull() bool {
	return operation == PullManifest || operation == PullBlob
}

// String returns the stable policy name of a resource.
func (resource Resource) String() string {
	switch resource {
	case ResourceRegistry:
		return "registry"
	case ResourceManifest:
		return "manifest"
	case ResourceBlob:
		return "blob"
	case ResourceBlobUpload:
		return "blob_upload"
	case ResourceTags:
		return "tags"
	case ResourceReferrers:
		return "referrers"
	case ResourceUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// String returns the HTTP method name.
func (method Method) String() string {
	if method == Unknown {
		return "unknown"
	}
	return string(method)
}
