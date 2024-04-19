# Zot registry vs Docker registry

## Context and Problem Statement

In order to start development of Harbor Satellite's first use case, we need to choose which local registry we want to use. While both (and other solutions) are viable, we want to focus on getting started quickly, so deployment and usability ease are our temporary focus.

## Considered Options

* [Zot registry](https://zotregistry.dev/v2.0.3/)
* [Docker registry](https://hub.docker.com/_/registry)

## Decision Outcome

Chosen option: " ", because after considering elements in the table below, it was the best choice.

| Feature                                | Docker Registry                           | Zot                             |
|----------------------------------------|-------------------------------------------|---------------------------------|
| Ease of setup                          | Very easy - with Docker, single CLI command | Easy/Very Easy - with binary (build from source or download) -> run with Docker / with Docker, single CLI command |
| Lightweight                            | Needs Docker                              | Can run as host-level service from single binary |
| Fully OCI compliant                    | Yes                                       | Yes                             |
| Built-in authentication                | Yes                                       | Yes                             |
| Garbage collection                     | Yes                                       | Yes                             |
| Storage deduplication                  | No                                        | Yes                             |
| Signing + verifying container images   | Docker Content Trust  (more complex)      | Cosign built-in                 |
| Attaching metadata to container images | "Annotations" can work similarly          | Notation built-in               |
| GUI                                    | No                                        | Yes                             |

[Interesting video about Zot](https://www.youtube.com/watch?v=zOjOF00aQSY&t=4s)
