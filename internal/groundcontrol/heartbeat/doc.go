// Package heartbeat buffers satellite heartbeats per replica and flushes
// them in batches (plan item 4.2, ADR-0016), so a beat costs no statement
// of its own. A flush never moves a value backwards: the store writes a
// beat only when it is not older than what is stored. A crash loses at
// most one flush interval of beats. It is part of gc-core and imports
// neither async nor authn.
package heartbeat
