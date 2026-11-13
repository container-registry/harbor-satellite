// Package heartbeat buffers satellite heartbeats per replica and flushes
// them in batches (plan item 4.2, ADR-0016), so a beat costs no statement
// of its own. A flush never moves a value backwards: the store writes a
// beat only when it is not older than what is stored. A failed flush is
// requeued, so a crash loses every beat buffered since the last successful
// flush: one flush interval while flushes succeed, more during a database
// outage. It is part of gc-core and imports neither async nor authn.
package heartbeat
