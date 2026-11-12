// Package webhook delivers fleet events to subscribed receivers as signed
// CloudEvents (plan item 9.1, ADR-0019). It reads event_outbox as an
// ordered log by (xid, id) with one checkpoint per subscription, so
// delivery is at least once and in order; the outbox id is the idempotency
// key. It imports none of async, authn or heartbeat.
package webhook
