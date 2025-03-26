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

## Alternatives

