---
status: proposed
date: 2025-03-29
deciders: [Harbor-Satellite Development Team]
consulted: [Harbor-Satellite Users, Architects]
informed: [Harbor-Satellite Developers, Operators]
---

# Remote Satellite Configuration Management

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
  - Allows to have one to many mapping for configs and satellites.
  - Ensures 1:1 mapping for Satellite and Configuration file.

- **Bad:**
  - Introduces additional complexity in configuration management.
  - Requires careful handling of rollback mechanisms.
  - Requires careful handling of fields that are mutable and fields that are not to be edited by the user.

This spawns a few more decisions to consider regarding the handling of the configuration file.

## Additional Decisions

### Storing Configuration in an Upstream Registry
- Configuration files will be stored in the upstream harbor registry inside config states similar to how we store the group states.
- The storing and updation of the config states will be handled by the Ground-Control.
- Ground Control will store validated configurations and provide a reference to satellites via the `satellite_state`.
- The satellite will fetch its configuration from an upstream registry similar to how it fetches group states.
- Each satellite will retrieve its assigned configuration state from the upstream registry and apply changes dynamically.
- This prevents duplication of configuration files across multiple satellites and allows for centralized management.

### Rollback Mechanisms
- The satellite will maintain the last known valid configuration on disk.
- If an updated configuration is found to be invalid (e.g., unreachable URLs, malformed JSON, etc.), the satellite will:
    - Roll back to the previous configuration stored on disk.
    - Report the rollback event and the erroneous configuration to Ground Control.
- Ensure that an invalid configuration is never stored on disk to prevent persistent failures.

### Mutability of Configuration Fields
- Some of the fields present in today's configuration file are supposed to be immutable by the user. Especially, the `state_config` field.
- It is proposed that we store the `state_config` in a separate file and the rest of the config in a separate file.
- Note that, any update to the config state is done only by the Ground-Control.
- The `state_config.json` is only for the reference of the user, the user cannot directly initate an update to this file.
- Updates to `config.json` can be initiated by the user, and the Ground-Control will update it accordingly.
- An example for the `state_config.json` and `config.json` is provided later in the document.

### Consequences
- **Good:**
    - Improves operational flexibility and automation.
    - Centralized storage ensures consistency across multiple satellites.

- **Bad:**
    - Introduces additional complexity in user-end for creating configuration files.
    - Increases the API surface for the Ground-Control.

## User Flow

### Creating a Configuration State
We will deal with the configuration as a different kind of state too, this means that we also need to create a configuration state before we reference
it within a satellite state. For this, we will send a request to ground-control that looks like so:
```bash
 curl --location 'http://localhost:8080/groups/sync' \
--header 'Content-Type: application/json' \
--data '{
  "config_name": "CONFIG_NAME",
  "registry": "YOUR_REGISTRY_URL",
  "config":
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
'
```
Once the ground-control receives the request, it shall parse the config and validate it statically. If there is an error, abort the operation
and let the user know that the configuration is erroneous. Further down the flow of events, the Satellite itself will perform a dynamic
validation to ensure that the configuration is valid.

### Registering a Satellite With Ground-Control
Since we now need to associate a configuration with each satellite, the way we associate groups, we need to add the upstream config that the satellite must
follow. The new register satellite request will look like:

```bash
curl --location 'http://localhost:8080/satellites/register' \
--header 'Content-Type: application/json' \
--data '{
    "name": "SATELLITE_NAME",
    "groups": ["GROUP_NAME"],
    "config": "CONFIG_NAME"
}'
```
The ground-control must provide API endpoints for the CRUD operations on configuration states as well, similar to what we
have with group states.

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
   - Satellite fetches it's `satellite_state` and from there, the configuration file from the upstream registry and configures itself accordingly.
        - Until it is able to access the remote configuration, the satellite shall use sane default values for the configuration file.
        - If the configuration is valid (i.e, the URL's are reachable and working):
            - Store the `config.json` on disk.
        - If the configuration is in-valid:
            - Roll back to the previous configuration that is already stored on-disk.
            - Report to the ground-control about the erroneous configuration and the rolled back configuration that the satellite
              is following.
            - **The satellite must never store an invalid configuration on-disk**
   - Uses the fetched state information to sync itself with the group states.

5. **Continuous Operation**
   - Periodically:
     - Fetches updates from its state artifact and syncs group states.
     - Applies configuration changes dynamically
   - In case the latest configuration is erroneous, the system rolls back to the previous healthy configuration.
     - Further, the Satellite should report back to the ground-control that the current config is erroneous and it has rolled back
       to a previous version of the configuration.
     - The request body may also contain the current config and the config that has been rolled back to.

From the user’s perspective, they only need to provide the satellite with its `token` and `ground_control_url`. From
there, they will have no need to interact with the satellite directly. The satellite will automatically fetch its state, configuration,
and keep attempting to reconcile at fixed intervals.

## New Configuration and Satellite State Formats

To support remote updates, the existing configuration structure needs to be refactored.

### `satellite_state`
The new satellite state will also contain a pointer to the configuration file that the satellite needs to follow. Much similar to how we deal
with the group states. This ensures reusability of the configuration, and prevents us from having to create a configuration for each satellite that
exists.
The new `satellite_state` might look something like:

```json
{
  "states": [...list of group states to follow],
  "config": "registry.com/satellite-config/config:latest"
}
```

### `state_config.json`
This is managed by ground control and is read-only for the satellite. This need not be stored in the upstream registry.
We may take the decision to store it at ground-control, in the future if we see a use case.

```json
{
    "auth": {<robot_account_credentials_and_upstream_url>},
    "state": "<satellite_state_to_follow>"
}
```

### `config.json`
This is managed by the user, via ground-control and may be edited by the satellite during run-time locally (e.g, for defaults). The decision to
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
