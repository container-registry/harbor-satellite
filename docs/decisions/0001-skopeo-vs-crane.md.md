# Skopeo vs Crane

## Context and Problem Statement

We want to decide wether to use Skopeo or Crane to replicate images from a remote source registry to a local destination registry.

## Considered Options

* [Skopeo](https://github.com/containers/skopeo)
* [Crane](https://github.com/google/go-containerregistry/tree/main/cmd/crane)

## Decision Outcome

Chosen option: "Crane", because according to our use cases _(listed in the table below)_, Crane works for all of them :

| Use Case | Skopeo | Crane |
|---|---|---|
| **Programmable, can be used as an SDK** | No (CLI only or via wrapping) | Yes (CLI + library) |
| **Configuration can be done via API** | No | Yes |
| **Move OCI Artifacts** | Yes (copy + delete) | Yes (copy + delete) |
| **Remapping Artifacts (programmatically)** _( = modify or move image)_ | No | Yes |
| **Image Consistency Verification** (was it really pulled correctly, is it present) | Yes (inspect manifest and compare hashes) | Yes (validate well-formed image + compare digest of image) |
| **Memory and CPU consumption on low end devices, Disk consumption** | "Very lightweight CPU usage" / 37.1MB | "Lightweight CPU usage" / 34.7MB |
| **Dealing with Low bandwidth networks, resume, etc.** | Can report transfer rate and progress per blob, can use `--retry`/`--retry-times` flags, evidence of people using it with [intermittent networking issues](https://github.com/containers/common/issues/654) | No resumable push -> works with single PATCH request, as per [this issue](https://github.com/google/go-containerregistry/issues/1448), can use `.withRetry`, support for [very slow speeds via buffering](https://github.com/google/go-containerregistry/issues/920) / [here](https://github.com/google/go-containerregistry/pull/923) |
