package proxy

// Operation identifies the OCI Distribution endpoint family being processed.
// The request method and Access field carry the specific action within that
// family, avoiding a separate operation constant for every upload phase.
type Operation uint8

const (
	UnknownOperation Operation = iota
	Ping
	Manifest
	Blob
	BlobUpload
	BlobMount
	Tags
	Referrers
)

// Access groups requests for authentication and coarse policy checks.
type Access uint8

const (
	UnknownAccess Access = iota
	ReadAccess
	WriteAccess
	DeleteAccess
)

// String returns the stable policy name of an endpoint family.
func (operation Operation) String() string {
	switch operation {
	case Ping:
		return "ping"
	case Manifest:
		return "manifest"
	case Blob:
		return "blob"
	case BlobUpload:
		return "blob_upload"
	case BlobMount:
		return "blob_mount"
	case Tags:
		return "tags"
	case Referrers:
		return "referrers"
	case UnknownOperation:
		return "unknown"
	default:
		return "unknown"
	}
}

// String returns the stable policy name of an access class.
func (access Access) String() string {
	switch access {
	case ReadAccess:
		return "read"
	case WriteAccess:
		return "write"
	case DeleteAccess:
		return "delete"
	case UnknownAccess:
		return "unknown"
	default:
		return "unknown"
	}
}
