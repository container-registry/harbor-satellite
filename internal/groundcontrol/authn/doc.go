// Package authn authenticates satellites with Ground Control session
// tokens (plan item 4.1, ADR-0016, gate G3). A token is "hss_" plus 32
// random bytes; Ground Control stores only its SHA-256 and looks it up by
// that hash. Sessions keeps a positive cache per token hash and per
// satellite name whose TTL, at most MaxSessionCacheTTL, bounds how long a
// revocation or an invalidation takes to reach every replica. It is part
// of gc-core: it sees the fleet ports only and imports neither async nor
// heartbeat.
package authn
