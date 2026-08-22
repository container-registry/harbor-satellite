// Package proxy provides Satellite's composable HTTP processing boundary for an
// OCI Distribution proxy.
//
// Stack adapts net/http requests into a validated Contract before configurable
// layers execute. The contract retains the original *http.Request and
// http.ResponseWriter and adds normalized repository, reference, digest, upload,
// endpoint-family, and access information. Layers therefore use standard HTTP
// streaming and cancellation directly without reparsing registry paths or storing
// request state in context values.
//
// The parser recognizes the OCI Distribution Specification v1.1.1 endpoint set.
// It scans fixed path suffixes from right to left so repository names may have
// arbitrary component depth within the compatibility limit, parses known query
// parameters once, and validates repository names, tags, digests, upload
// identifiers, and methods. Docker-specific extensions such as the catalog
// endpoint are not accepted, and legacy Docker version headers are not emitted.
//
// Handler processes one Contract. Layer decorates a Handler and controls whether
// processing continues by invoking the next handler. New composes layers once in
// declaration order over an identity handler, so an empty stack is valid and no
// mandatory terminal or base handler is required. A layer may enrich the contract,
// reject a request, satisfy it without delegation, or wrap work around subsequent
// layers. When and Predicate support conditional composition without coupling the
// stack to authentication, policy, metadata, caching, auditing, or forwarding
// implementations.
//
// Errors returned by handlers are serialized using only error codes defined by OCI
// Distribution v1.1.1. Unexpected internal errors receive a plain HTTP 500 response
// rather than a custom wire code. The composed chain is immutable after New; each
// request receives its own mutable Contract, while services shared by layers remain
// responsible for their own concurrency safety.
package proxy
