---
status: proposed
date: 2025-03-29
deciders: [Harbor-Satellite Development Team]
consulted: [Harbor-Satellite Users, Architects]
informed: [Harbor-Satellite Developers, Operators]
---

# Remote Config Injection

## Context and Problem Statement

Currently, we provide a static `config.json` file to the satellite. As we move forward, we may want to update the satellite’s configuration remotely. This is not a straightforward change and impacts a lot of the existing code. This proposal outlines approaches to solve this problem.

## Decision Drivers

- The need for dynamic configuration updates without manual intervention at the satellite level.
- Ensuring that erroneous configurations do not break the satellite.
- Ensuring clear distinction between user mutable and user immutable fields.
- Providing the satellite with only the bare necessary information at start up.

## Considered Options

- **Option 1:** Remote configuration updates with rollback capability.
- **Option 2:** Keep using static `config.json` but allow periodic polling of external updates.
- **Option 3:** Use environment variables or command-line arguments for configuration.

## Decision Outcome

**Chosen option:** "Remote configuration updates with rollback capability" because it allows seamless updates, maintains system stability, and enables future scalability.

### Consequences

- **Good:**
  - Allows remote updates without requiring manual intervention.
  - Provides rollback mechanisms for erroneous configurations.
  - Improves operational flexibility and automation.

- **Bad:**
  - Introduces additional complexity in configuration management.
  - Requires careful handling of rollback mechanisms.
  - Requires careful handling of fields that are mutable and fields that are not to be edited by the user.

## Flow of Events

1. **Startup Initialization**
   The Satellite starts with only two inputs. At start up, these may be provided as environment variables or command line arguments:
   - `token` (Authentication token)
   - `ground_control_url` (URL of Ground Control)

2. **Perform ZTR**
   - The Satellite sends a request to Ground Control with its token.
   - Ground Control authenticates the Satellite and responds with:
     - Upstream registry credentials (username, password, etc.).
     - State artifact location in the upstream registry.
   - The Satellite stores the `state_config` information on disk since it is critical for operation.

3. **Fetch State Artifact**
   - The Satellite authenticates with the upstream registry using the received credentials.
   - It retrieves its state artifact, which contains:
     - The latest configuration.
     - The list of group state artifacts.

4. **Load Config & Reconcile State**
   - Satellite receives the configuration file from the upstream registry and configures itself accordingly.
        - Until it is able to access the remote configuration, the satellite shall use sane default values for the configuration file.
        - Though not as critical as the `state_config.json`, `config.json` may also be stored on disk to ensure consistency.
   - Uses the fetched state information to sync itself with the group states.

5. **Continuous Operation**
   - Periodically:
     - Fetches updates from its state artifact and syncs group states.
     - Applies configuration changes dynamically
   - In case the latest configuration is erroneous, the system rolls back to the previous healthy configuration.

From the user’s perspective, they only need to provide the satellite with its `token` and `ground_control_url`. From
there, they will have no need to interact with the satellite directly. The satellite will automatically fetch its state, configuration,
and keep attempting to reconcile at fixed intervals.

## New Configuration Format

To support remote updates, the existing configuration structure needs to be refactored.

### `state_config.json`
This is managed by ground control and is read-only for the user and the satellite.

```json
{
    "auth": {},
    "state": ""
}
```

### `config.json`
This is managed by the user and may be edited by the satellite during run-time (e.g, for defaults). The decision to
include `ground_control_url` was made since it feels like a valid use case that the user wants to change the ground control endpoint
after the initial deployment.

```json
{
    "ground_control_url": "http://127.0.0.1:8080",
    "log_level": "info",
    "use_unsecure": true,
    "zot_config_path": "./registry/config.json",
    "state_replication_interval": "@every 00h00m10s",
    "update_config_interval": "@every 00h00m10s",
    "register_satellite_interval": "@every 00h00m10s",
    "bring_own_registry": false,
    "local_registry": {
        "url": "http://127.0.0.1:8585",
        "username": "",
        "password": ""
    }
}
```

