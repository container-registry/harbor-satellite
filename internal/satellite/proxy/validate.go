package proxy

import (
	"net/url"
	"strings"
)

const (
	// 0x20 is ASCII space; smaller bytes are the C0 control characters.
	asciiSpace byte = 0x20
	// 0x7f is ASCII DEL; larger bytes are outside 7-bit ASCII.
	asciiDelete byte = 0x7f
	// Several OCI clients limit <registry-host>/<name> to 255 characters.
	maxRegistryRepositoryLength = 255
)

func canonicalPath(requestURL *url.URL) (string, error) {
	requestPath := requestURL.Path
	if requestPath == "" {
		return "", invalidPath(requestPath, "request path is required")
	}
	escapedPath := requestURL.EscapedPath()
	if requestURL.RawPath != "" || strings.ContainsRune(escapedPath, '%') {
		return "", invalidPath(requestPath, "encoded path is not canonical")
	}
	if strings.ContainsRune(requestPath, '\\') || strings.Contains(requestPath, "//") {
		return "", invalidPath(requestPath, "ambiguous path separator")
	}
	for _, pathByte := range []byte(requestPath) {
		if pathByte < asciiSpace || pathByte >= asciiDelete {
			return "", invalidPath(requestPath, "path must contain printable ASCII")
		}
	}
	for remaining := requestPath; remaining != ""; {
		segment, rest, found := strings.Cut(remaining, "/")
		if segment == "." || segment == ".." {
			return "", invalidPath(requestPath, "path traversal segment")
		}
		if !found {
			break
		}
		remaining = rest
	}
	return requestPath, nil
}

// validRepository implements the OCI repository-name grammar and ensures that
// the client-facing <registry-host>/<name> value does not exceed 255 characters:
//
//	[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*
//	(/[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*)*
//
// Grammar: https://specs.opencontainers.org/distribution-spec/?v=v1.1.1#DISTRIBUTION-SPEC-27
// Implementer's Note:  https://specs.opencontainers.org/distribution-spec/?v=v1.1.1#DISTRIBUTION-SPEC-28
func validRepository(registryHost, repository string) bool {
	if registryHost == "" || repository == "" ||
		(len(registryHost)+1+len(repository)) > maxRegistryRepositoryLength {
		return false
	}
	for {
		component, remaining, found := strings.Cut(repository, "/")
		if !validRepositoryComponent(component) {
			return false
		}
		if !found {
			return true
		}
		if remaining == "" {
			return false
		}
		repository = remaining
	}
}

// validRepositoryComponent validates one slash-delimited component of the
// repository grammar: [a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*.
func validRepositoryComponent(component string) bool {
	if component == "" || !lowerAlphaNumeric(component[0]) {
		return false
	}
	for index := 1; index < len(component); {
		if lowerAlphaNumeric(component[index]) {
			index++
			continue
		}
		switch component[index] {
		case '.', '_':
			separator := component[index]
			index++
			if separator == '_' && index < len(component) && component[index] == '_' {
				index++
			}
		case '-':
			for index < len(component) && component[index] == '-' {
				index++
			}
		default:
			return false
		}
		if index >= len(component) || !lowerAlphaNumeric(component[index]) {
			return false
		}
	}
	return true
}

// validTag implements the OCI tag-name grammar:
//
//	[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,127}
//
// Refer: https://specs.opencontainers.org/distribution-spec/?v=v1.1.1#DISTRIBUTION-SPEC-29
// Grammar: https://specs.opencontainers.org/distribution-spec/?v=v1.1.1#DISTRIBUTION-SPEC-30
func validTag(tag string) bool {
	if len(tag) == 0 || len(tag) > 128 || !tagFirst(tag[0]) {
		return false
	}
	for index := 1; index < len(tag); index++ {
		if !tagCharacter(tag[index]) {
			return false
		}
	}
	return true
}

func validOpaqueSegment(value string) bool {
	if value == "" {
		return false
	}
	for _, valueByte := range []byte(value) {
		// Upload IDs are opaque but must remain one visible ASCII path segment.
		// Space/control bytes can make logs and headers ambiguous; slash and
		// backslash can be interpreted as path separators by other components.
		if valueByte <= asciiSpace || valueByte >= asciiDelete ||
			valueByte == '/' || valueByte == '\\' {
			return false
		}
	}
	return true
}

func lowerAlphaNumeric(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
}

func tagFirst(value byte) bool {
	return value >= 'A' && value <= 'Z' || lowerAlphaNumeric(value) || value == '_'
}

func tagCharacter(value byte) bool {
	return tagFirst(value) || value == '.' || value == '-'
}
