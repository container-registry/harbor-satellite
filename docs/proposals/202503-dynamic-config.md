# Remote Config Injection

- Author(s):
    - mviswanathsai

- Related Ticket(s):
    - [#104](https://github.com/container-registry/harbor-satellite/issues/104)

## Why
Currently, we provide a static config.json file to the satellite. As we move forward, we may want to be able to update the satellite’s config remotely. This is not a straightforward change and it changes a lot of the existing code. This proposal outlines approaches that we may want to consider to solve this problem.

## Pitfalls of the current solution
Static config is fine, but in the long run, the needs of the user might change and more configurable fields might arise. This requires us to provide the ground control manager with the ability to update the satellite config dynamically.

## Goals
- Outline the use flow of dynamic configuration
- List the expectations from dynamic configuration
- Provide possible solutions to achieve the expectations

## Audience
- Developers of Harbor-Satellite
- Users of Harbor-Satellite

## Non-Goals
- Not meant as a technical guide for implementation

## How
This document should answer the following questions:
1. The Flow of Events
2. The New Configuration Format
3. Handling Erroneous Configuration

### The Flow of Events
The flow of events will look like so:
1. Startup Initialization
    The Satellite starts with only two inputs:
        - token (Authentication token)
        - ground_control_url (URL of Ground Control)


2. Perform ZTR
    The Satellite sends a request to Ground Control with its token.
    Ground Control authenticates the Satellite and responds with:
        - Upstream registry credentials (username, password, etc.).
        - State artifact location in the upstream registry.
        - The satellite stores the `state_config` information on disk, since it is critical for the satellite's operation.


3. Fetch State Artifact
    The Satellite authenticates with the upstream registry using the received credentials.
    It retrieves its state artifact, which contains:
        - The latest configuration.
        - The list of group state artifacts.


4. Load Config & Reconcile State
    The Satellite parses the config.json.
    Uses the provided state information to sync itself with the group states.


5. Continuous Operation
    Periodically:
        - Fetches updates from its state artifact.
        - Syncs group states.
        - Applies configuration changes dynamically.
    In the case that the latest version of the configuration is erroneous, roll back to the previous healthy configuration.

From the ground-control user’s perspective, they just need to provide the satellite with it’s token and the location of the ground-control. From there, they will have no need to interact with the satellite directly. The satellite will automatically fetch its state, configuration and keep attempting to reconcile at fixed intervals.

### The New Configuration Format
For the above flow of events, the existing configuration file needs to be refactored a bit to look like this:

`state_config` field is only updated by the ground-control and not by the user directly. This
could mean that we can also keep the `state_config` inside a separate file called `state_config.json`
as this is a read-only field that is present for the user's reference.
`state_config.json`
```json
{
    "auth": {},
    "state": ""
}
```

`config.json`
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
        "password": "",
    }
}
```
The main change here (apart from moving `state_config`) is that we have removed the `token` field. Why? The token is ephemeral, once used to perform ZTR,
it serves no other purpose. We can safely remove this from the `config.json`.

Since `ground_control_url` and `token` are fields required at start up, we can either provide them either as environment
variables, or command line arguments. We will keep the `ground_control_url` in the config file since changing the `ground_control_url`
after some time is a valid use case, whereas token is ephemeral and is only used once at startup during ZTR.


### Handling Erroneous Configuration
There is a possibility that the newly pulled configuration is erroneous. The satellite should be able to detect erroneous configurations
and roll back to a previous, healthy configuration when the newly pulled configuration is erroneous.

The satellite may store the previous configuration in memory, or in a separate volume. Storing it in memory is a much easier and effective
solution, since we may not have the privilege of configuring a volume for the satellite. For the pilot implementation, an in-memory copy of
the previous configuration should be good enough. For the future, we can start considering the option to allow the user to configure the
satellite to store the previous configuration to disk.

Further, since we are using harbor to store the satellite state (and in turn, the configuration files), we may be able to checkpoint and
revert to an earlier version of the configuration upstream even if there isn't an earlier version available in memory.

The flow for this would be like so:
 1. Fetch New Configuration
   - Satellite retrieves the latest `config.json` from its state artifact in Harbor.

2. Validate Configuration
   - Before applying, the Satellite validates the new configuration:
     - Ensure required fields exist (`auth`, `states`, `jobs`, etc.).
     - Verify JSON is parsable and correctly formatted.

3. If the Configuration is Valid
   - Apply the new configuration.
   - Store the previous configuration in memory for rollback if needed.
   - Continue normal operation.

4. If the Configuration is Erroneous
   - Revert to the previous in-memory configuration.
   - If no in-memory copy exists, check the Harbor registry for a previous version.
   - If no previous version is found:
     - Enter fail-safe mode with minimal operation.
     - Log errors and retry fetching later.

## Conclusion
By shifting from a static configuration file to a dynamic, remotely managed configuration, we improve flexibility and adaptability for users while ensuring seamless updates.

Key takeaways:
- The satellite initializes with minimal input and fetches its configuration dynamically.
- A new structured format is introduced, separating state-related information from user-modifiable configuration.
- The satellite follows a structured approach for handling erroneous configurations, ensuring resilience through in-memory rollbacks and upstream checkpointing.

For resilience, we now propose that `state_config` must be stored on-disk and the `config.json` may be stored in-memory or on-disk.

## Alternatives

