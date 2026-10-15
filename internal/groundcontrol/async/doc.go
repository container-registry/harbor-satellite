// Package async is Ground Control's async plane (plan item 2, ADR-0014).
// API transactions append fleet events to an outbox; the publisher claims
// them and runs level-triggered reconcilers. A reconciler never trusts the
// event: it renders the aggregate's desired state from the database and
// publishes it when the rendered digest differs from the published one, so
// duplicated, reordered and lost-then-replayed events converge to the same
// result. Nothing keeps state between events in memory, so any number of
// workers may run.
package async
