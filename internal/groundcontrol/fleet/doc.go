// Package fleet is Ground Control's domain core (plan item 1.2): the fleet
// types, the services behind the admin and satellite endpoints, and the
// ports through which they reach the database, the registry and the
// workload identity provider. It imports no adapter: those live under
// internal/groundcontrol/adapters, recording fakes in fleet/fleettest, and
// internal/groundcontrol/app wires them together.
package fleet
